package ui

import (
	"strings"
	"testing"

	"github.com/shhac/prtea/internal/config"
	"github.com/shhac/prtea/internal/github"
)

func pollTestApp() App {
	return App{
		prList:       NewPRListModel(TabToReview),
		appConfig:    config.Defaults(),
		knownPRs:     make(map[string]bool),
		pollEnabled:  true,
		pollInterval: config.Defaults().PollIntervalDuration(),
	}
}

func TestHandlePollTick(t *testing.T) {
	t.Run("disabled polling schedules nothing", func(t *testing.T) {
		app := pollTestApp()
		app.pollEnabled = false
		if _, cmd := app.handlePollTick(); cmd != nil {
			t.Error("disabled polling must not schedule a tick")
		}
	})

	t.Run("zero interval schedules nothing", func(t *testing.T) {
		app := pollTestApp()
		app.pollInterval = 0
		if _, cmd := app.handlePollTick(); cmd != nil {
			t.Error("zero interval must not schedule a tick (tight-loop hazard)")
		}
	})

	t.Run("client not ready still reschedules", func(t *testing.T) {
		app := pollTestApp()
		if _, cmd := app.handlePollTick(); cmd == nil {
			t.Error("polling must reschedule while the client is not ready")
		}
	})

	t.Run("ready client fetches and reschedules", func(t *testing.T) {
		app := pollTestApp()
		app.ghClient = &fakeGitHubService{}
		app.prList.state = stateLoaded
		if _, cmd := app.handlePollTick(); cmd == nil {
			t.Error("ready poll must fetch + reschedule")
		}
	})
}

func TestHandlePRsLoadedSnapshotsOnlyFirstLoad(t *testing.T) {
	app := pollTestApp()
	prs := []github.PRItem{{Number: 1, Repo: github.Repo{Owner: "o", Name: "r"}}}

	model, _ := app.handlePRsLoaded(PRsLoadedMsg{ToReview: prs})
	app = model.(App)

	if !app.initialLoadDone {
		t.Error("first load must set initialLoadDone")
	}
	if !app.knownPRs["o/r#1"] {
		t.Errorf("first load must snapshot known PRs, got %v", app.knownPRs)
	}
}

func TestHandlePollPRsLoadedDetectsBeforeSnapshotting(t *testing.T) {
	app := pollTestApp()
	app.notifyEnabled = true
	app.knownPRs["o/r#1"] = true

	newPR := github.PRItem{Number: 2, Repo: github.Repo{Owner: "o", Name: "r"}}
	model, _ := app.handlePollPRsLoaded(pollPRsLoadedMsg{ToReview: []github.PRItem{
		{Number: 1, Repo: github.Repo{Owner: "o", Name: "r"}},
		newPR,
	}})
	app = model.(App)

	// The new PR must end up in the snapshot; detection ran before it (a
	// reversed order would have found nothing new and still snapshot).
	if !app.knownPRs["o/r#2"] {
		t.Errorf("poll load must snapshot new PRs, got %v", app.knownPRs)
	}
	if len(app.detectNewPRs([]github.PRItem{newPR})) != 0 {
		t.Error("after snapshotting, the PR must no longer be 'new'")
	}
}

func TestRenderInputSingleUserPrompt(t *testing.T) {
	panel := NewChatPanelModel()
	panel.SetSize(80, 40)
	panel.chatMode = ChatModeInsert
	panel.textInput.Focus()

	rendered := panel.renderInput()
	if got := strings.Count(rendered, "> "); got != 1 {
		t.Errorf("insert-mode input renders %d '> ' prompts, want exactly 1: %q", got, rendered)
	}
}

func TestFetchCommentsCmdJoinsResolution(t *testing.T) {
	client := &fakeGitHubService{
		inlineComments: []github.InlineComment{
			{ID: 1, Path: "a.go", Line: 3},
			{ID: 2, Path: "a.go", Line: 9},
		},
		resolution: map[int64]bool{1: true},
	}

	msg := fetchCommentsCmd(client, "o", "r", 7)()
	loaded, ok := msg.(CommentsLoadedMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommentsLoadedMsg", msg)
	}
	if loaded.Err != nil {
		t.Fatalf("unexpected error: %v", loaded.Err)
	}
	if !loaded.InlineComments[0].Resolved {
		t.Error("comment 1 should be marked resolved")
	}
	if loaded.InlineComments[1].Resolved {
		t.Error("comment 2 should stay unresolved")
	}
}

func TestFetchCommentsCmdResolutionErrorIsBestEffort(t *testing.T) {
	client := &fakeGitHubService{
		inlineComments: []github.InlineComment{{ID: 1, Path: "a.go", Line: 3}},
		resolutionErr:  errForTest("graphql down"),
	}

	msg := fetchCommentsCmd(client, "o", "r", 7)()
	loaded, ok := msg.(CommentsLoadedMsg)
	if !ok {
		t.Fatalf("msg = %T, want CommentsLoadedMsg", msg)
	}
	if loaded.Err != nil {
		t.Fatalf("resolution failure must not fail the comments load: %v", loaded.Err)
	}
	if len(loaded.InlineComments) != 1 || loaded.InlineComments[0].Resolved {
		t.Errorf("comments must load unresolved when resolution fails: %+v", loaded.InlineComments)
	}
}
