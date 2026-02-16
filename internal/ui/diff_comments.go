package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/claude"
	"github.com/shhac/prtea/internal/github"
)

// handleCommentModeKey processes key events while comment input mode is active.
func (m *DiffViewerModel) handleCommentModeKey(msg tea.KeyMsg) (DiffViewerModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.commentMode = false
		m.commentInput.SetValue("")
		m.commentInput.Blur()
		m.cancelSelection()
		m.refreshContent()
		return *m, nil
	case "enter":
		body := strings.TrimSpace(m.commentInput.Value())
		path := m.commentTargetFile
		line := m.commentTargetLine
		startLine := m.commentTargetStartLine
		m.commentMode = false
		m.commentInput.Blur()
		m.cancelSelection()
		m.refreshContent()
		return *m, func() tea.Msg {
			return InlineCommentAddMsg{Path: path, Line: line, Body: body, StartLine: startLine}
		}
	default:
		var cmd tea.Cmd
		m.commentInput, cmd = m.commentInput.Update(msg)
		return *m, cmd
	}
}

// SetAIInlineComments stores AI-generated inline comments and rebuilds the diff cache.
func (m *DiffViewerModel) SetAIInlineComments(comments []claude.InlineReviewComment) {
	m.aiInlineComments = comments
	m.aiCommentsByFileLine = make(map[string][]claude.InlineReviewComment)
	for _, c := range comments {
		key := commentKey(c.Path, c.Line)
		m.aiCommentsByFileLine[key] = append(m.aiCommentsByFileLine[key], c)
	}
	// Full cache invalidation since comment lines change hunk sizes
	m.cachedLines = nil
	m.cachedLineInfo = nil
	m.refreshContent()
}

// ClearAIInlineComments removes all AI inline comments.
func (m *DiffViewerModel) ClearAIInlineComments() {
	m.aiInlineComments = nil
	m.aiCommentsByFileLine = nil
	m.cachedLines = nil
	m.cachedLineInfo = nil
	m.refreshContent()
}

// SetPendingInlineComments stores pending comments and rebuilds the diff cache.
func (m *DiffViewerModel) SetPendingInlineComments(comments []PendingInlineComment) {
	m.pendingCommentsByFileLine = make(map[string][]PendingInlineComment)
	for _, c := range comments {
		key := commentKey(c.Path, c.Line)
		m.pendingCommentsByFileLine[key] = append(m.pendingCommentsByFileLine[key], c)
	}
	m.cachedLines = nil
	m.cachedLineInfo = nil
	m.refreshContent()
}

// SetGitHubInlineComments stores GitHub review comments, groups them into threads,
// and rebuilds the diff cache so they render at their line positions.
func (m *DiffViewerModel) SetGitHubInlineComments(comments []github.InlineComment) {
	if len(comments) == 0 {
		m.ghCommentThreads = nil
		m.cachedLines = nil
		m.cachedLineInfo = nil
		m.refreshContent()
		return
	}

	// Separate root comments from replies; index roots by ID for thread building.
	rootByID := make(map[int64]*ghCommentThread)
	var rootOrder []int64 // preserve insertion order
	var replies []github.InlineComment

	for _, c := range comments {
		if c.Outdated {
			continue // outdated comments stay in Comments tab only
		}
		if c.InReplyToID != 0 {
			replies = append(replies, c)
		} else {
			t := ghCommentThread{Root: c}
			rootByID[c.ID] = &t
			rootOrder = append(rootOrder, c.ID)
		}
	}

	// Attach replies to their root threads, sorted chronologically.
	sort.Slice(replies, func(i, j int) bool {
		return replies[i].CreatedAt.Before(replies[j].CreatedAt)
	})
	for _, r := range replies {
		if t, ok := rootByID[r.InReplyToID]; ok {
			t.Replies = append(t.Replies, r)
		}
		// Orphan replies (root not found) are silently dropped — they
		// still appear in the Comments tab flat list.
	}

	// Build the "path:line" → threads map.
	m.ghCommentThreads = make(map[string][]ghCommentThread)
	for _, id := range rootOrder {
		t := rootByID[id]
		key := commentKey(t.Root.Path, t.Root.Line)
		m.ghCommentThreads[key] = append(m.ghCommentThreads[key], *t)
	}

	m.cachedLines = nil
	m.cachedLineInfo = nil
	m.refreshContent()
}

// EnterCommentMode activates comment input mode targeting the cursor line.
// If the cursor is on a non-commentable line, it snaps to the nearest commentable
// line within the same hunk. Returns nil if no commentable line is found.
// When a multi-line selection is active, the comment targets the full range.
func (m *DiffViewerModel) EnterCommentMode() tea.Cmd {
	if len(m.hunks) == 0 || m.activeTab != TabDiff || len(m.cachedLineInfo) == 0 {
		return nil
	}

	// Find the comment target from cursor position
	targetLine, targetFile := m.commentTargetFromCursor()
	if targetLine == 0 || targetFile == "" {
		return nil
	}

	m.commentTargetFile = targetFile
	m.commentTargetLine = targetLine
	m.commentTargetStartLine = 0

	// If a multi-line selection is active, resolve the range
	if m.selectionAnchor >= 0 {
		startLine, endLine := m.resolveSelectionRange()
		if startLine > 0 && endLine > 0 && startLine != endLine {
			// GitHub API requires start_line < line
			if startLine > endLine {
				startLine, endLine = endLine, startLine
			}
			m.commentTargetStartLine = startLine
			m.commentTargetLine = endLine
		}
	}

	m.commentMode = true

	// Pre-fill if editing existing comment at this location
	key := commentKey(m.commentTargetFile, m.commentTargetLine)
	if comments, ok := m.pendingCommentsByFileLine[key]; ok && len(comments) > 0 {
		m.commentInput.SetValue(comments[0].Body)
		m.commentInput.CursorEnd()
	} else {
		m.commentInput.SetValue("")
	}

	m.refreshContent()
	return m.commentInput.Focus()
}

// resolveSelectionRange finds the commentable new-side line numbers at the
// boundaries of the current multi-line selection. Returns (startLine, endLine)
// where both are new-side file line numbers, or (0, 0) if no valid range found.
func (m *DiffViewerModel) resolveSelectionRange() (int, int) {
	lo, hi := m.selectionRange()
	if lo < 0 {
		return 0, 0
	}

	// Find first commentable line from selection start (forward)
	startLine := 0
	for i := lo; i <= hi; i++ {
		if i < len(m.cachedLineInfo) && m.cachedLineInfo[i].isCommentable && m.cachedLineInfo[i].newLineNum > 0 {
			startLine = m.cachedLineInfo[i].newLineNum
			break
		}
	}

	// Find last commentable line from selection end (backward)
	endLine := 0
	for i := hi; i >= lo; i-- {
		if i < len(m.cachedLineInfo) && m.cachedLineInfo[i].isCommentable && m.cachedLineInfo[i].newLineNum > 0 {
			endLine = m.cachedLineInfo[i].newLineNum
			break
		}
	}

	return startLine, endLine
}

// commentTargetFromCursor returns the file path and line number for the cursor's
// current position. If the cursor is on a non-commentable line, searches nearby
// lines in the same hunk for the nearest commentable one.
func (m *DiffViewerModel) commentTargetFromCursor() (int, string) {
	if m.cursorLine < 0 || m.cursorLine >= len(m.cachedLineInfo) {
		return 0, ""
	}

	info := m.cachedLineInfo[m.cursorLine]
	if info.isCommentable && info.newLineNum > 0 {
		return info.newLineNum, info.filename
	}

	// Cursor is on a non-commentable line (@@, -, \, etc.)
	// Search forward then backward within the same hunk
	hunk := info.hunkIdx
	if hunk < 0 {
		return 0, ""
	}

	// Forward
	for i := m.cursorLine + 1; i < len(m.cachedLineInfo); i++ {
		ci := m.cachedLineInfo[i]
		if ci.hunkIdx != hunk {
			break
		}
		if ci.isCommentable && ci.newLineNum > 0 {
			return ci.newLineNum, ci.filename
		}
	}
	// Backward
	for i := m.cursorLine - 1; i >= 0; i-- {
		ci := m.cachedLineInfo[i]
		if ci.hunkIdx != hunk {
			break
		}
		if ci.isCommentable && ci.newLineNum > 0 {
			return ci.newLineNum, ci.filename
		}
	}

	return 0, ""
}

// buildCommentOverlayMsg gathers context from the current cursor position
// and returns a ShowCommentOverlayMsg, or nil if no commentable line is found.
// When a multi-line selection is active, only threads whose line range exactly
// matches the selection are shown; otherwise a blank thread is opened.
func (m *DiffViewerModel) buildCommentOverlayMsg() *ShowCommentOverlayMsg {
	if len(m.cachedLineInfo) == 0 {
		return nil
	}
	targetLine, targetFile := m.commentTargetFromCursor()
	if targetLine == 0 || targetFile == "" {
		return nil
	}

	// Resolve multi-line selection range (0, 0 if no selection)
	var startLine, endLine int
	if m.selectionAnchor >= 0 {
		startLine, endLine = m.resolveSelectionRange()
		if startLine > 0 && endLine > 0 && startLine != endLine {
			if startLine > endLine {
				startLine, endLine = endLine, startLine
			}
			// Override target to match the selection range endpoints
			targetLine = endLine
		} else {
			startLine = 0
		}
	}

	// Extract diff context lines from the hunk
	hunkIdx := m.cachedLineInfo[m.cursorLine].hunkIdx
	if hunkIdx < 0 || hunkIdx >= len(m.hunks) {
		return nil
	}
	hunk := m.hunks[hunkIdx]

	// Find target line index within hunk and extract a window around it
	targetIdx := -1
	newLine := 0
	for i, line := range hunk.Lines {
		if strings.HasPrefix(line, "@@") {
			// Parse start line from @@ header
			var n int
			if _, err := fmt.Sscanf(line, "@@ -%*d,%*d +%d", &n); err == nil {
				newLine = n - 1
			}
			continue
		}
		if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, `\`) {
			newLine++
		}
		if newLine == targetLine {
			targetIdx = i
			break
		}
	}
	if targetIdx < 0 {
		targetIdx = 0
	}

	ctxStart := max(0, targetIdx-2)
	ctxEnd := min(len(hunk.Lines), targetIdx+3)
	diffLines := hunk.Lines[ctxStart:ctxEnd]

	key := commentKey(targetFile, targetLine)

	// Gather threads. For multi-line selections, only include threads
	// whose line range exactly matches the selection.
	var ghThreads []ghCommentThread
	var aiComments []claude.InlineReviewComment
	var pendingComments []PendingInlineComment

	if startLine > 0 {
		// Multi-line selection: exact range match only
		for _, t := range m.ghCommentThreads[key] {
			if t.Root.StartLine == startLine && t.Root.Line == endLine {
				ghThreads = append(ghThreads, t)
			}
		}
		// AI and pending comments with matching StartLine
		for _, c := range m.aiCommentsByFileLine[key] {
			if c.StartLine == startLine && c.Line == endLine {
				aiComments = append(aiComments, c)
			}
		}
		for _, c := range m.pendingCommentsByFileLine[key] {
			if c.StartLine == startLine && c.Line == endLine {
				pendingComments = append(pendingComments, c)
			}
		}
	} else {
		// Single-line: match all threads at this line (existing behavior)
		ghThreads = m.ghCommentThreads[key]
		aiComments = m.aiCommentsByFileLine[key]
		pendingComments = m.pendingCommentsByFileLine[key]
	}

	return &ShowCommentOverlayMsg{
		Path:            targetFile,
		Line:            targetLine,
		StartLine:       startLine,
		DiffLines:       diffLines,
		TargetLineInCtx: targetIdx - ctxStart,
		GHThreads:       ghThreads,
		AIComments:      aiComments,
		PendingComments: pendingComments,
	}
}

// IsCommenting returns true when the comment input is actively being typed into.
func (m DiffViewerModel) IsCommenting() bool {
	return m.commentMode
}

// renderCommentBar renders the comment input bar shown during comment mode.
func (m DiffViewerModel) renderCommentBar() string {
	var target string
	if m.commentTargetStartLine > 0 {
		target = fmt.Sprintf("%s:%d-%d", m.commentTargetFile, m.commentTargetStartLine, m.commentTargetLine)
	} else {
		target = fmt.Sprintf("%s:%d", m.commentTargetFile, m.commentTargetLine)
	}
	promptStyle := lipgloss.NewStyle().Foreground(commentBoxPendingBorder).Bold(true)
	prompt := promptStyle.Render("📝 " + target + " > ")
	return prompt + m.commentInput.View()
}

// commentBoxMaxPreviewLines is the maximum body lines shown in the inline preview.
const commentBoxMaxPreviewLines = 3

// renderCommentBox renders content inside a bordered box, split into viewport lines.
// header is the first line inside the box (e.g. "💬 Claude AI").
// body is the pre-rendered content (already glamour-processed or plain text).
// borderColor is the lipgloss color for the rounded border.
// highlighted uses a thick border and brighter color to indicate cursor targeting.
// gutter is the left margin prefix for each line (e.g. "▎ " for focused hunk).
func (m *DiffViewerModel) renderCommentBox(header, body string, borderColor lipgloss.Color, highlighted bool, gutter string) []string {
	boxWidth := m.viewport.Width - 2 // 2-char gutter
	if boxWidth < 14 {
		boxWidth = 14
	}

	// Assemble content: header + body
	var content strings.Builder
	content.WriteString(header)
	if body != "" {
		content.WriteString("\n")
		// Trim and apply preview limit
		bodyLines := strings.Split(body, "\n")
		// Remove trailing empty lines from glamour output
		for len(bodyLines) > 0 && strings.TrimSpace(bodyLines[len(bodyLines)-1]) == "" {
			bodyLines = bodyLines[:len(bodyLines)-1]
		}
		if len(bodyLines) > commentBoxMaxPreviewLines {
			remaining := len(bodyLines) - commentBoxMaxPreviewLines
			bodyLines = bodyLines[:commentBoxMaxPreviewLines]
			bodyLines = append(bodyLines, commentBoxTrimStyle.Render(fmt.Sprintf("[+%d lines]", remaining)))
		}
		content.WriteString(strings.Join(bodyLines, "\n"))
	}

	// Add [c] hint on last line of content
	hintStyle := commentBoxHintStyle
	if highlighted {
		hintStyle = commentBoxHintHiStyle
	}
	content.WriteString("  " + hintStyle.Render("[c]"))

	border := lipgloss.RoundedBorder()
	if highlighted {
		border = lipgloss.ThickBorder()
	}

	boxStyle := lipgloss.NewStyle().
		Border(border).
		BorderForeground(borderColor).
		Width(boxWidth - 2). // -2 for border chars
		PaddingLeft(1).PaddingRight(1)

	rendered := boxStyle.Render(content.String())

	// Split into viewport lines and prepend gutter
	result := strings.Split(rendered, "\n")
	for i, line := range result {
		result[i] = gutter + line
	}
	return result
}

// renderGHCommentThread renders a single GitHub comment thread inside a bordered box.
func (m *DiffViewerModel) renderGHCommentThread(t ghCommentThread, highlighted bool, gutter string) []string {
	boxInnerWidth := m.viewport.Width - 2 - 2 - 2 // gutter, border, padding
	if boxInnerWidth < 10 {
		boxInnerWidth = 10
	}

	// Header: 💬 @author · Jan 2 15:04
	header := commentBoxHeaderStyle.Render("💬 @"+t.Root.Author.Login) +
		commentBoxMetaStyle.Render(" · "+t.Root.CreatedAt.Format("Jan 2 15:04"))

	// Build body: root body + replies
	var body strings.Builder
	body.WriteString(m.renderMarkdown(t.Root.Body, boxInnerWidth))

	for i, r := range t.Replies {
		if i >= 1 {
			// Trim after first reply
			remaining := len(t.Replies) - 1
			body.WriteString("\n")
			body.WriteString(commentBoxTrimStyle.Render(fmt.Sprintf("[+%d more replies]", remaining)))
			break
		}
		body.WriteString("\n")
		replyHeader := commentBoxReplyStyle.Render("↳ ") +
			commentBoxHeaderStyle.Render("@"+r.Author.Login) +
			commentBoxMetaStyle.Render(" · "+r.CreatedAt.Format("Jan 2 15:04"))
		body.WriteString(replyHeader)
		body.WriteString("\n")
		body.WriteString(m.renderMarkdown(r.Body, boxInnerWidth))
	}

	borderColor := commentBoxGitHubBorder
	if highlighted {
		borderColor = commentBoxGitHubBorderHi
	}
	return m.renderCommentBox(header, body.String(), borderColor, highlighted, gutter)
}

// injectInlineComments appends any inline comment boxes (AI, GitHub, pending) that
// are attached to the given file:line. It returns the augmented lines and infos slices.
func (m *DiffViewerModel) injectInlineComments(
	lines []string, infos []lineInfo,
	hunkIdx int, filename string, newLine int,
	isFocused bool, cursorTargetKey string,
) ([]string, []lineInfo) {
	key := commentKey(filename, newLine)
	boxInnerWidth := m.viewport.Width - 2 - 2 - 2
	if boxInnerWidth < 10 {
		boxInnerWidth = 10
	}
	isTargeted := cursorTargetKey != "" && key == cursorTargetKey

	commentGutter := "  "
	if isFocused {
		commentGutter = diffFocusGutterStyle.Render("▎") + " "
	}

	// AI inline comments
	if comments, ok := m.aiCommentsByFileLine[key]; ok {
		for _, c := range comments {
			header := commentBoxHeaderStyle.Render("💬 Claude AI")
			body := m.renderMarkdown(c.Body, boxInnerWidth)
			borderColor := commentBoxAIBorder
			if isTargeted {
				borderColor = commentBoxAIBorderHi
			}
			boxLines := m.renderCommentBox(header, body, borderColor, isTargeted, commentGutter)
			for range boxLines {
				infos = append(infos, lineInfo{hunkIdx: hunkIdx, filename: filename, comment: commentAI})
			}
			lines = append(lines, boxLines...)
		}
	}

	// GitHub inline comments (threaded)
	if threads, ok := m.ghCommentThreads[key]; ok {
		for _, t := range threads {
			threadLines := m.renderGHCommentThread(t, isTargeted, commentGutter)
			for range threadLines {
				infos = append(infos, lineInfo{hunkIdx: hunkIdx, filename: filename, comment: commentGitHub})
			}
			lines = append(lines, threadLines...)
		}
	}

	// Pending inline comments (user + AI drafts)
	if comments, ok := m.pendingCommentsByFileLine[key]; ok {
		for _, c := range comments {
			source := "Draft"
			if c.Source == "ai" {
				source = "Draft (AI)"
			}
			header := commentBoxHeaderStyle.Render("📝 " + source)
			body := m.renderMarkdown(c.Body, boxInnerWidth)
			borderColor := commentBoxPendingBorder
			if isTargeted {
				borderColor = commentBoxPendingBorderHi
			}
			boxLines := m.renderCommentBox(header, body, borderColor, isTargeted, commentGutter)
			for range boxLines {
				infos = append(infos, lineInfo{hunkIdx: hunkIdx, filename: filename, comment: commentPending})
			}
			lines = append(lines, boxLines...)
		}
	}

	return lines, infos
}
