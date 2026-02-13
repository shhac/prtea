package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	number  int
	title   string
	repo    string // short repo name (e.g. "api")
	owner   string // repo owner (e.g. "shhac")
	repoFull string // full name (e.g. "shhac/api")
	author  string
	adds    int
	dels    int
	files   int
	htmlURL string
}

func (i PRItem) FilterValue() string { return i.title }
func (i PRItem) Title() string       { return fmt.Sprintf("#%d %s", i.number, i.title) }
func (i PRItem) Description() string {
	return fmt.Sprintf("%s · %s · +%d/-%d · %d files", i.author, i.repo, i.adds, i.dels, i.files)
}

// PRSelectedMsg is sent when the user selects a PR.
type PRSelectedMsg struct {
	Owner  string
	Repo   string
	Number int
}

// PRRefreshMsg is sent when the user presses `r` to refresh PR data.
type PRRefreshMsg struct{}

// PRListModel manages the PR list panel.
type PRListModel struct {
	list      list.Model
	activeTab PRListTab
	width     int
	height    int
	focused   bool

	// Data state
	state    loadState
	errMsg   string
	toReview []list.Item
	myPRs    []list.Item
}

func NewPRListModel() PRListModel {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true

	l := list.New(nil, delegate, 0, 0)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()

	return PRListModel{
		list:      l,
		activeTab: TabToReview,
		state:     stateLoading,
	}
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
						Owner:  item.owner,
						Repo:   item.repo,
						Number: item.number,
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
