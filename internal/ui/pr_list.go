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

// PRItem represents a PR in the list.
type PRItem struct {
	number int
	title  string
	repo   string
	author string
	adds   int
	dels   int
	files  int
}

func (i PRItem) FilterValue() string { return i.title }
func (i PRItem) Title() string       { return fmt.Sprintf("#%d %s", i.number, i.title) }
func (i PRItem) Description() string {
	return fmt.Sprintf("%s · %s · +%d/-%d · %d files", i.author, i.repo, i.adds, i.dels, i.files)
}

// PRSelectedMsg is sent when the user selects a PR.
type PRSelectedMsg struct {
	Number int
}

// PRListModel manages the PR list panel.
type PRListModel struct {
	list      list.Model
	activeTab PRListTab
	width     int
	height    int
	focused   bool
}

func NewPRListModel() PRListModel {
	items := placeholderPRItems()
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true

	l := list.New(items, delegate, 0, 0)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()

	return PRListModel{
		list:      l,
		activeTab: TabToReview,
	}
}

func (m PRListModel) Update(msg tea.Msg) (PRListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, PRListKeys.PrevTab):
			if m.activeTab == TabMyPRs {
				m.activeTab = TabToReview
			}
			return m, nil
		case key.Matches(msg, PRListKeys.NextTab):
			if m.activeTab == TabToReview {
				m.activeTab = TabMyPRs
			}
			return m, nil
		case key.Matches(msg, PRListKeys.Select):
			if item, ok := m.list.SelectedItem().(PRItem); ok {
				return m, func() tea.Msg {
					return PRSelectedMsg{Number: item.number}
				}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
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
	content := m.list.View()

	inner := lipgloss.JoinVertical(lipgloss.Left, header, content)

	style := panelStyle(m.focused, false, m.width-2, m.height-2)
	return style.Render(inner)
}

func (m PRListModel) renderTabs() string {
	var tabs []string

	if m.activeTab == TabToReview {
		tabs = append(tabs, activeTabStyle().Render("To Review"))
		tabs = append(tabs, inactiveTabStyle().Render("My PRs"))
	} else {
		tabs = append(tabs, inactiveTabStyle().Render("To Review"))
		tabs = append(tabs, activeTabStyle().Render("My PRs"))
	}

	return strings.Join(tabs, " ")
}

func placeholderPRItems() []list.Item {
	return []list.Item{
		PRItem{number: 1234, title: "Fix authentication timeout", repo: "api", author: "alice", adds: 42, dels: 8, files: 3},
		PRItem{number: 1235, title: "Add rate limiting middleware", repo: "api", author: "bob", adds: 156, dels: 12, files: 5},
		PRItem{number: 892, title: "Update dashboard charts", repo: "frontend", author: "carol", adds: 89, dels: 34, files: 7},
		PRItem{number: 456, title: "Migrate to new ORM", repo: "backend", author: "dave", adds: 312, dels: 245, files: 18},
		PRItem{number: 789, title: "Fix memory leak in worker", repo: "worker", author: "eve", adds: 23, dels: 5, files: 2},
		PRItem{number: 321, title: "Add search functionality", repo: "frontend", author: "frank", adds: 198, dels: 15, files: 9},
		PRItem{number: 654, title: "Update CI pipeline", repo: "infra", author: "grace", adds: 45, dels: 30, files: 4},
	}
}
