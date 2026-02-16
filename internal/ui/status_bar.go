package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StatusBarModel renders the bottom status bar.
type StatusBarModel struct {
	width      int
	focused    Panel
	mode       AppMode
	selectedPR int
	filtering     bool // true when PR list filter input is active
	diffSearching bool // true when diff viewer search input is active
	diffSearchInfo string // e.g. "3/17" when search has matches

	// Temporary flash message (e.g. "Refreshing PR #123...")
	statusMessage string
	// Monotonic counter: incremented on each SetTemporaryMessage call.
	// StatusBarClearMsg carries the seq at time of scheduling; if it doesn't
	// match current seq the clear is stale and ignored.
	messageSeq int
}

func NewStatusBarModel() StatusBarModel {
	return StatusBarModel{}
}

func (m *StatusBarModel) SetWidth(width int) {
	m.width = width
}

func (m *StatusBarModel) SetState(focused Panel, mode AppMode) {
	m.focused = focused
	m.mode = mode
}

// SetFiltering updates whether the PR list filter input is active.
func (m *StatusBarModel) SetFiltering(filtering bool) {
	m.filtering = filtering
}

// SetDiffSearching updates whether the diff viewer search input is active.
func (m *StatusBarModel) SetDiffSearching(searching bool) {
	m.diffSearching = searching
}

// SetDiffSearchInfo updates the diff search match info (e.g. "3/17").
func (m *StatusBarModel) SetDiffSearchInfo(info string) {
	m.diffSearchInfo = info
}

func (m *StatusBarModel) SetSelectedPR(number int) {
	m.selectedPR = number
}

// SetTemporaryMessage shows a flash message in the status bar.
// Returns a tea.Cmd that will send a StatusBarClearMsg after the given duration,
// which the caller must include in the returned command batch.
func (m *StatusBarModel) SetTemporaryMessage(msg string, duration time.Duration) tea.Cmd {
	m.messageSeq++
	m.statusMessage = msg
	seq := m.messageSeq
	return tea.Tick(duration, func(_ time.Time) tea.Msg {
		return StatusBarClearMsg{Seq: seq}
	})
}

// ClearMessage explicitly clears the temporary message.
func (m *StatusBarModel) ClearMessage() {
	m.statusMessage = ""
}

// ClearIfSeqMatch clears the message only if the given seq matches the current one.
// Returns true if the message was cleared.
func (m *StatusBarModel) ClearIfSeqMatch(seq int) bool {
	if seq == m.messageSeq {
		m.statusMessage = ""
		return true
	}
	return false
}

func (m StatusBarModel) View() string {
	var leftHints string
	if m.statusMessage != "" {
		leftHints = " " + m.statusMessage
	} else {
		leftHints = m.keyHints()
	}
	rightInfo := m.contextInfo()

	leftRendered := statusBarAccentStyle.Render(leftHints)
	rightRendered := statusBarStyle.Render(rightInfo)

	leftWidth := lipgloss.Width(leftRendered)
	rightWidth := lipgloss.Width(rightRendered)
	padding := m.width - leftWidth - rightWidth
	if padding < 0 {
		padding = 0
	}

	bar := leftRendered +
		statusBarStyle.Render(strings.Repeat(" ", padding)) +
		rightRendered

	return statusBarStyle.Width(m.width).Render(bar)
}

func (m StatusBarModel) keyHints() string {
	if m.filtering {
		return " [Esc]cancel [Enter]apply [type]filter"
	}

	if m.mode == ModeInsert {
		return " [Enter]send [Esc]exit insert"
	}

	switch m.focused {
	case PanelLeft:
		return " [h/l]tab [j/k]move [/]filter [Enter]select [r]refresh [Tab]panel [z]zoom [?]help"
	case PanelCenter:
		if m.diffSearching {
			return " [Esc]cancel [Enter]confirm [type]search"
		}
		if m.diffSearchInfo != "" {
			return " [n/N]next/prev match [Esc]clear search [/]new search [Tab]panel [?]help"
		}
		return " [h/l]tab [j/k]scroll [n/N]hunk [s/Space]select [S]file [c]comments [/]search [r]refresh [Tab]panel [z]zoom [?]help"
	case PanelRight:
		return " [h/l]tab [j/k]scroll [Enter]insert [C]new chat [r]refresh [Tab]panel [z]zoom [?]help"
	default:
		return " [Tab]panel [Ctrl+R]review [?]help [q]quit"
	}
}

func (m StatusBarModel) contextInfo() string {
	modeStr := ""
	switch m.mode {
	case ModeInsert:
		modeStr = " INSERT "
	case ModeOverlay:
		modeStr = " OVERLAY "
	case ModeCommand:
		modeStr = " COMMAND "
	default:
		modeStr = " NAV "
	}

	prInfo := ""
	if m.selectedPR > 0 {
		prInfo = fmt.Sprintf("PR #%d ", m.selectedPR)
	}

	return modeStr + prInfo
}
