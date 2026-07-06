package ui

// AI turn lifecycle: everything between the chat panel and the ai package —
// starting turns, pumping engine events, persisting threads, and executing
// user-confirmed actions through the GitHub client.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/ai"
	"github.com/shhac/prtea/internal/config"
	"github.com/shhac/prtea/internal/github"
)

// startOrient sends the canned "orient me" prompt into the chat thread.
func (m App) startOrient() (tea.Model, tea.Cmd) {
	if m.session == nil {
		m.chatPanel.SetChatError("No PR selected. Select a PR first.")
		m.chatPanel.SetActiveTab(ChatTabChat)
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}
	if m.aiEngine == nil {
		m.chatPanel.SetChatError(codexMissingMsg)
		m.chatPanel.SetActiveTab(ChatTabChat)
		m.showAndFocusPanel(PanelRight)
		return m, nil
	}
	if m.chatPanel.IsChatWaiting() {
		return m, nil
	}

	m.chatPanel.SetActiveTab(ChatTabChat)
	m.showAndFocusPanel(PanelRight)
	m.chatPanel.StartChatWaiting("Orient me on this PR")
	cmd := m.startAITurn(ai.OrientMessage)
	return m, tea.Batch(cmd, m.chatPanel.spinner.Tick)
}

// codexMissingMsg is shown when AI features are used without the codex CLI.
const codexMissingMsg = "codex CLI not found.\nInstall it from https://github.com/openai/codex and sign in."

// persistThread saves the current session's thread ID and transcript to disk.
func (m *App) persistThread() {
	s := m.session
	if s == nil {
		return
	}
	msgs := m.chatPanel.ChatMessages()
	if s.ThreadID == "" && len(msgs) == 0 {
		return
	}
	_ = m.threadStore.Put(s.Owner, s.Repo, s.Number, s.ThreadID, msgs)
}

// recordThreadID persists a freshly created thread ID, even when the user has
// already navigated away from the PR that started it — otherwise the next
// message would silently start a new thread.
func (m *App) recordThreadID(owner, repo string, number int, threadID string) {
	if m.session.MatchesPR(number) {
		m.session.ThreadID = threadID
		m.persistThread()
		return
	}
	var msgs []ai.Message
	if cached, err := m.threadStore.Get(owner, repo, number); err == nil && cached != nil {
		msgs = cached.Messages
	}
	_ = m.threadStore.Put(owner, repo, number, threadID, msgs)
}

// startAITurn kicks off an engine turn for the current session and returns the
// command that pumps its events into the update loop. The caller is
// responsible for having recorded the user message in the chat panel.
func (m App) startAITurn(prompt string) tea.Cmd {
	s := m.session
	s.CancelAITurn()

	// The engine is immutable; the per-turn deadline lives on this context.
	ctx, cancel := context.WithTimeout(context.Background(), m.appConfig.AITimeoutDuration())
	ch := make(aiEventChan)

	engine := m.aiEngine
	owner, repo, number := s.Owner, s.Repo, s.Number
	title := s.Title
	threadID := s.ThreadID
	files := s.DiffFiles
	comments := s.Comments
	inline := s.InlineComments
	selectedHunks := m.diffViewer.GetSelectedHunkContent()

	go func() {
		defer close(ch)

		forward := func(ev ai.Event) bool {
			return ai.Emit(ctx, ch, AIEventMsg{Owner: owner, Repo: repo, PRNumber: number, Event: ev})
		}

		var events <-chan ai.Event
		var err error
		if threadID == "" {
			customPrompt, _ := config.GetRepoPrompt(owner, repo)
			events, err = engine.StartThread(ctx, ai.ThreadInput{
				Owner:        owner,
				Repo:         repo,
				PRNumber:     number,
				PRContext:    buildThreadContext(title, files, comments, inline, selectedHunks),
				CustomPrompt: customPrompt,
				RepoPath:     ai.FindLocalCheckout(owner, repo),
				Message:      prompt,
			})
		} else {
			events, err = engine.Send(ctx, threadID, ai.MessageInput{
				Message:      prompt,
				ContextDelta: buildContextDelta(selectedHunks),
			})
		}
		if err != nil {
			forward(ai.Event{Kind: ai.EventError, Text: err.Error()})
			return
		}
		for ev := range events {
			if !forward(ev) {
				return
			}
		}
	}()

	s.AIEventCh = ch
	s.AICancel = cancel
	return listenForStream(ch)
}

// handleChatSend validates state and kicks off an AI turn for a typed message.
// The chat panel has already recorded the user message and entered waiting.
func (m App) handleChatSend(message string) (tea.Model, tea.Cmd) {
	if m.session == nil {
		m.chatPanel.SetChatError("No PR selected. Select a PR first.")
		return m, nil
	}
	if m.aiEngine == nil {
		m.chatPanel.SetChatError(codexMissingMsg)
		return m, nil
	}
	return m, tea.Batch(m.startAITurn(message), m.chatPanel.spinner.Tick)
}

// -- AI turn handlers --

// handleAIEvent processes one engine event from the in-flight turn.
func (m App) handleAIEvent(msg AIEventMsg) (tea.Model, tea.Cmd) {
	// A freshly created thread ID is persisted even if the user has moved on
	// to another PR — otherwise the next message would start a new thread.
	if msg.Event.Kind == ai.EventThreadStarted {
		m.recordThreadID(msg.Owner, msg.Repo, msg.PRNumber, msg.Event.ThreadID)
	}
	if m.session == nil || m.session.AIEventCh == nil || !m.session.MatchesPR(msg.PRNumber) {
		return m, nil
	}

	switch msg.Event.Kind {
	case ai.EventThinking:
		m.chatPanel.AddActivity("· " + firstLine(msg.Event.Text))

	case ai.EventCommandStarted:
		m.chatPanel.AddActivity("▸ " + displayCommand(msg.Event.Command))

	case ai.EventCommandCompleted:
		if msg.Event.ExitCode != 0 {
			m.chatPanel.AddActivity(fmt.Sprintf("▸ %s (exit %d)", displayCommand(msg.Event.Command), msg.Event.ExitCode))
		}

	case ai.EventMessage:
		m.chatPanel.AddResponse(msg.Event.Text)

	case ai.EventActionProposal:
		m.chatPanel.SetPendingActions(msg.Event.Actions)
		// Leave insert mode so y/n confirm keys work immediately.
		m.chatPanel.ExitInsertMode()
		m.setMode(ModeNavigation)

	case ai.EventError:
		m.chatPanel.SetChatError(msg.Event.Text)
		m.session.CancelAITurn()
		m.persistThread()
		return m, nil

	case ai.EventDone:
		m.chatPanel.SetTurnDone()
		m.session.CancelAITurn()
		m.persistThread()
		return m, nil
	}

	return m, listenForStream(m.session.AIEventCh)
}

// handleActionRespond executes or dismisses proposed actions the chat panel
// reported as confirmed/dismissed.
func (m App) handleActionRespond(msg AIActionRespondMsg) (tea.Model, tea.Cmd) {
	if m.session == nil || len(msg.Actions) == 0 {
		return m, nil
	}

	if !msg.Approve {
		m.chatPanel.AddActivity("✗ Action dismissed")
		m.persistThread()
		return m, nil
	}
	if m.ghClient == nil {
		m.chatPanel.SetChatError("GitHub client not ready")
		return m, nil
	}

	cmds := make([]tea.Cmd, len(msg.Actions))
	for i, action := range msg.Actions {
		cmds[i] = executeActionCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number, action)
	}
	return m, tea.Batch(cmds...)
}

// handleActionResult records the outcome of an executed action.
func (m App) handleActionResult(msg AIActionResultMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.chatPanel.AddActivity("✗ " + msg.Description + " failed")
		m.persistThread()
		clearCmd := m.statusBar.SetTemporaryMessage(
			fmt.Sprintf("✗ %s: %s", msg.Description, formatUserError(msg.Err.Error())), 5*time.Second)
		return m, clearCmd
	}

	m.chatPanel.AddActivity("✓ " + msg.Description)
	m.persistThread()
	clearCmd := m.statusBar.SetTemporaryMessage("✓ "+msg.Description, 3*time.Second)
	var refreshCmds []tea.Cmd
	refreshCmds = append(refreshCmds, clearCmd)
	if m.session != nil && m.ghClient != nil {
		refreshCmds = append(refreshCmds,
			fetchCommentsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number),
			fetchReviewsCmd(m.ghClient, m.session.Owner, m.session.Repo, m.session.Number),
		)
	}
	return m, tea.Batch(refreshCmds...)
}

// executeActionCmd executes a user-confirmed AI-proposed action via the
// GitHub client and reports the outcome.
func executeActionCmd(client GitHubService, owner, repo string, number int, action ai.Action) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		switch action.Type {
		case ai.ActionPostComment:
			err = client.PostComment(ctx, owner, repo, number, action.Body)
		case ai.ActionReplyToComment:
			err = client.ReplyToComment(ctx, owner, repo, number, action.CommentID, action.Body)
		case ai.ActionSubmitReview:
			if reviewAction, ok := reviewActionFromValue(action.Event); ok {
				err = submitSimpleReview(ctx, client, owner, repo, number, reviewAction, action.Body)
			} else {
				err = fmt.Errorf("unknown review event %q", action.Event)
			}
		default:
			err = fmt.Errorf("unknown action type %q", action.Type)
		}
		return AIActionResultMsg{Description: action.Describe(), Err: err}
	}
}

// listenForStream returns a tea.Cmd that reads the next engine event from the
// active turn's channel.
func listenForStream(ch <-chan AIEventMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// -- Context builders --

// maxContextDiffBytes caps the diff embedded in a thread's context so a huge
// PR cannot blow the model's context window. When exceeded, the diff is
// truncated with a notice; codex can still explore a local checkout.
const maxContextDiffBytes = 256 * 1024

// buildThreadContext assembles the PR context for a new AI thread:
// title, diff, comments (with IDs so the agent can propose replies),
// and any hunks the user has selected.
func buildThreadContext(title string, files []github.PRFile, comments []github.Comment, inline []github.InlineComment, selectedHunks string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", title)

	if len(files) > 0 {
		b.WriteString("\nDiff:\n\n")
		diff := buildDiffContent(files)
		if len(diff) > maxContextDiffBytes {
			diff = diff[:maxContextDiffBytes] + "\n\n[... diff truncated — explore the local checkout for the rest ...]\n"
		}
		b.WriteString(diff)
	} else {
		b.WriteString("\n(Diff not yet loaded)\n")
	}

	if len(comments) > 0 {
		b.WriteString("\nPR comments:\n")
		for _, c := range comments {
			fmt.Fprintf(&b, "- %s: %s\n", c.Author.Login, c.Body)
		}
	}

	if len(inline) > 0 {
		b.WriteString("\nInline review comments (id — file:line — author; [resolved] marks settled threads):\n")
		for _, c := range inline {
			status := ""
			if c.Resolved {
				status = " [resolved]"
			}
			fmt.Fprintf(&b, "- %d — %s:%d — %s%s: %s\n", c.ID, c.Path, c.Line, c.Author.Login, status, c.Body)
		}
	}

	if selectedHunks != "" {
		b.WriteString("\nThe user has selected these hunks to focus on:\n\n")
		b.WriteString(selectedHunks)
	}

	return b.String()
}

// buildContextDelta assembles the refreshed-context block for a follow-up
// message on an existing thread. Empty when there is nothing new to add.
func buildContextDelta(selectedHunks string) string {
	if selectedHunks == "" {
		return ""
	}
	return "The user has selected these hunks to focus on:\n\n" + selectedHunks
}

// buildDiffContent constructs a unified diff string from PR files.
func buildDiffContent(files []github.PRFile) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(fmt.Sprintf("--- a/%s\n", f.Filename))
		b.WriteString(fmt.Sprintf("+++ b/%s\n", f.Filename))
		if f.Patch != "" {
			b.WriteString(f.Patch)
			b.WriteString("\n")
		} else {
			b.WriteString("(binary or too large to display)\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// displayCommand compacts an agent shell command for the activity feed,
// stripping the `sh -lc "..."` wrapper codex uses.
func displayCommand(cmd string) string {
	if idx := strings.Index(cmd, " -lc "); idx != -1 {
		cmd = strings.Trim(strings.TrimSpace(cmd[idx+5:]), `"'`)
	}
	return firstLine(cmd)
}

// firstLine returns the first line of a possibly multi-line string.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx != -1 {
		return s[:idx] + " …"
	}
	return s
}
