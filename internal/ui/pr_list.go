package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// PRListTab identifies which sub-tab is active.
type PRListTab int

const (
	TabToReview PRListTab = iota
	TabMyPRs
)

// loadState tracks the data-fetch lifecycle.
type loadState int

const (
	stateLoading loadState = iota
	stateLoaded
	stateError
)

// PRItem represents a PR in the list.
type PRItem struct {
	number   int
	title    string
	repo     string // short repo name (e.g. "api")
	owner    string // repo owner (e.g. "shhac")
	repoFull string // full name (e.g. "shhac/api")
	author   string
	htmlURL  string
}

func (i PRItem) FilterValue() string { return i.title }
func (i PRItem) Title() string       { return fmt.Sprintf("#%d %s", i.number, i.title) }
func (i PRItem) Description() string {
	return fmt.Sprintf("%s · %s", i.author, i.repo)
}

// PRSelectedMsg is sent when the user selects a PR.
type PRSelectedMsg struct {
	Owner   string
	Repo    string
	Number  int
	HTMLURL string
}

// PRRefreshMsg is sent when the user presses `r` to refresh PR data.
type PRRefreshMsg struct{}

// prItemDelegate renders PR list items with distinct cursor and selected states.
// The cursor (Bubbletea's Index()) uses the stock left-border style.
// The "selected" PR (loaded in diff/chat) gets a ▸ marker prefix.
type prItemDelegate struct {
	selectedPRNumber *int // points to PRListModel.selectedPRNumber
}

func (d prItemDelegate) Height() int                             { return 2 }
func (d prItemDelegate) Spacing() int                            { return 1 }
func (d prItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d prItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(PRItem)
	if !ok {
		return
	}
	if m.Width() <= 0 {
		return
	}

	title := i.Title()
	desc := i.Description()

	isCursor := index == m.Index()
	isActive := d.selectedPRNumber != nil && *d.selectedPRNumber != 0 && i.number == *d.selectedPRNumber

	// Truncate text to fit — leave 2 chars for prefix (▸ or padding)
	textWidth := m.Width() - 4
	if textWidth < 1 {
		textWidth = 1
	}
	title = ansi.Truncate(title, textWidth, "…")
	desc = ansi.Truncate(desc, textWidth, "…")

	switch {
	case isCursor && isActive:
		// Cursor on the active/loaded PR: left border + accent color
		titleStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("62")).
			Foreground(lipgloss.Color("62")).
			Bold(true).
			Padding(0, 0, 0, 1)
		descStyle := titleStyle.Bold(false).Foreground(lipgloss.Color("99"))
		title = titleStyle.Render(title)
		desc = descStyle.Render(desc)
	case isCursor:
		// Cursor on a non-active PR: stock Bubbletea selected style
		titleStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.AdaptiveColor{Light: "#F793FF", Dark: "#AD58B4"}).
			Foreground(lipgloss.AdaptiveColor{Light: "#EE6FF8", Dark: "#EE6FF8"}).
			Padding(0, 0, 0, 1)
		descStyle := titleStyle.Foreground(lipgloss.AdaptiveColor{Light: "#F793FF", Dark: "#AD58B4"})
		title = titleStyle.Render(title)
		desc = descStyle.Render(desc)
	case isActive:
		// Active/loaded PR without cursor: ▸ marker in accent color
		marker := lipgloss.NewStyle().Foreground(lipgloss.Color("62")).Bold(true).Render("▸ ")
		titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Padding(0, 0, 0, 2)
		title = marker + titleStyle.Render(title)
		desc = descStyle.Render(desc)
	default:
		// Normal item
		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"}).
			Padding(0, 0, 0, 2)
		descStyle := titleStyle.Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})
		title = titleStyle.Render(title)
		desc = descStyle.Render(desc)
	}

	fmt.Fprintf(w, "%s\n%s", title, desc)
}

// PRListModel manages the PR list panel.
type PRListModel struct {
	list      list.Model
	activeTab PRListTab
	width     int
	height    int
	focused   bool

	// Tracks the PR currently loaded in diff/chat (0 = none).
	// Heap-allocated so the delegate's pointer survives value copies.
	selectedPRNumber *int

	// Data state
	state    loadState
	errMsg   string
	toReview []list.Item
	myPRs    []list.Item
}

func NewPRListModel() PRListModel {
	selected := new(int) // heap-allocated, shared with delegate

	delegate := prItemDelegate{selectedPRNumber: selected}

	l := list.New(nil, delegate, 0, 0)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()

	return PRListModel{
		list:             l,
		activeTab:        TabToReview,
		state:            stateLoading,
		selectedPRNumber: selected,
	}
}

// SetSelectedPR marks which PR is currently loaded in the diff/chat panels.
func (m *PRListModel) SetSelectedPR(number int) {
	*m.selectedPRNumber = number
}

// SetLoading puts the panel into loading state.
func (m *PRListModel) SetLoading() {
	m.state = stateLoading
	m.errMsg = ""
}

// SetError puts the panel into error state with a message.
func (m *PRListModel) SetError(err string) {
	m.state = stateError
	m.errMsg = err
}

// SetItems populates both tab datasets and switches to the loaded state.
func (m *PRListModel) SetItems(toReview, myPRs []list.Item) {
	m.toReview = toReview
	m.myPRs = myPRs
	m.state = stateLoaded
	m.errMsg = ""

	// Show the active tab's data
	switch m.activeTab {
	case TabToReview:
		m.list.SetItems(m.toReview)
	case TabMyPRs:
		m.list.SetItems(m.myPRs)
	}
}

func (m PRListModel) Update(msg tea.Msg) (PRListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, PRListKeys.PrevTab):
			if m.activeTab == TabMyPRs {
				m.activeTab = TabToReview
				if m.state == stateLoaded {
					m.list.SetItems(m.toReview)
				}
			}
			return m, nil
		case key.Matches(msg, PRListKeys.NextTab):
			if m.activeTab == TabToReview {
				m.activeTab = TabMyPRs
				if m.state == stateLoaded {
					m.list.SetItems(m.myPRs)
				}
			}
			return m, nil
		case key.Matches(msg, PRListKeys.Select):
			if item, ok := m.list.SelectedItem().(PRItem); ok {
				return m, func() tea.Msg {
					return PRSelectedMsg{
						Owner:   item.owner,
						Repo:    item.repo,
						Number:  item.number,
						HTMLURL: item.htmlURL,
					}
				}
			}
		case key.Matches(msg, PRListKeys.Refresh):
			m.state = stateLoading
			return m, func() tea.Msg {
				return PRRefreshMsg{}
			}
		}
	}

	// Only delegate to the inner list when we have data
	if m.state == stateLoaded {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *PRListModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Account for borders (2), header (2), padding
	innerWidth := width - 4
	innerHeight := height - 5
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}
	m.list.SetSize(innerWidth, innerHeight)
}

func (m *PRListModel) SetFocused(focused bool) {
	m.focused = focused
}

func (m PRListModel) View() string {
	header := m.renderTabs()

	var content string
	switch m.state {
	case stateLoading:
		content = m.renderLoading()
	case stateError:
		content = m.renderError()
	case stateLoaded:
		content = m.list.View()
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, header, content)

	style := panelStyle(m.focused, false, m.width-2, m.height-2)
	return style.Render(inner)
}

func (m PRListModel) renderTabs() string {
	var tabs []string

	toReviewLabel := "To Review"
	myPRsLabel := "My PRs"

	if m.state == stateLoaded {
		toReviewLabel = fmt.Sprintf("To Review (%d)", len(m.toReview))
		myPRsLabel = fmt.Sprintf("My PRs (%d)", len(m.myPRs))
	}

	if m.activeTab == TabToReview {
		tabs = append(tabs, activeTabStyle().Render(toReviewLabel))
		tabs = append(tabs, inactiveTabStyle().Render(myPRsLabel))
	} else {
		tabs = append(tabs, inactiveTabStyle().Render(toReviewLabel))
		tabs = append(tabs, activeTabStyle().Render(myPRsLabel))
	}

	return strings.Join(tabs, " ")
}

func (m PRListModel) renderLoading() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Padding(1, 2).
		Render("Loading PRs...")
}

func (m PRListModel) renderError() string {
	msg := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Bold(true).
		Padding(1, 2).
		Render(m.errMsg)

	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Padding(0, 2).
		Render("Press r to retry")

	return lipgloss.JoinVertical(lipgloss.Left, msg, hint)
}
