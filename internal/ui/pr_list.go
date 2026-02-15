package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
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

func (i PRItem) FilterValue() string {
	return i.title + " " + i.author + " " + i.repoFull + " " + i.owner + " " + i.repo
}
func (i PRItem) Title() string       { return fmt.Sprintf("#%d %s", i.number, i.title) }
func (i PRItem) Description() string {
	return fmt.Sprintf("%s · %s", i.author, i.repo)
}

// prItemDelegate renders PR list items with distinct cursor and selected states.
// The cursor (Bubbletea's Index()) uses the stock left-border style.
// The "selected" PR (loaded in diff/chat) gets a ▸ marker prefix.
type prItemDelegate struct {
	selectedPRNumber *int    // points to PRListModel.selectedPRNumber
	ciOverallStatus  *string // points to PRListModel.ciOverallStatus
	reviewDecision   *string // points to PRListModel.reviewDecision
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

	// Compute CI + review badges for the active/selected PR
	var badges string
	badgeWidth := 0
	if isActive {
		if d.ciOverallStatus != nil && *d.ciOverallStatus != "" {
			b, w := ciBadgeForList(*d.ciOverallStatus)
			badges += b
			badgeWidth += w
		}
		if d.reviewDecision != nil && *d.reviewDecision != "" {
			b, w := reviewBadgeForList(*d.reviewDecision)
			badges += b
			badgeWidth += w
		}
	}

	// Truncate text to fit — leave 2 chars for prefix (▸ or padding)
	textWidth := m.Width() - 4
	if textWidth < 1 {
		textWidth = 1
	}
	descWidth := textWidth - badgeWidth
	if descWidth < 1 {
		descWidth = 1
	}
	title = ansi.Truncate(title, textWidth, "…")
	desc = ansi.Truncate(desc, descWidth, "…")

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

	fmt.Fprintf(w, "%s\n%s%s", title, desc, badges)
}

// PRListModel manages the PR list panel.
type PRListModel struct {
	list      list.Model
	spinner   spinner.Model
	activeTab PRListTab
	width     int
	height    int
	focused   bool

	// Tracks the PR currently loaded in diff/chat (0 = none).
	// Heap-allocated so the delegate's pointer survives value copies.
	selectedPRNumber *int

	// CI status for the selected PR (heap-allocated, shared with delegate).
	ciOverallStatus *string

	// Review decision for the selected PR (heap-allocated, shared with delegate).
	reviewDecision *string

	// Data state
	state    loadState
	errMsg   string
	toReview []list.Item
	myPRs    []list.Item
}

func NewPRListModel() PRListModel {
	selected := new(int)       // heap-allocated, shared with delegate
	ciStatus := new(string)    // heap-allocated, shared with delegate
	reviewDec := new(string)   // heap-allocated, shared with delegate

	delegate := prItemDelegate{
		selectedPRNumber: selected,
		ciOverallStatus:  ciStatus,
		reviewDecision:   reviewDec,
	}

	l := list.New(nil, delegate, 0, 0)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.FilterInput.Placeholder = "title, author, repo…"
	l.DisableQuitKeybindings()

	return PRListModel{
		list:             l,
		spinner:          newLoadingSpinner(),
		activeTab:        TabToReview,
		state:            stateLoading,
		selectedPRNumber: selected,
		ciOverallStatus:  ciStatus,
		reviewDecision:   reviewDec,
	}
}

// SetSelectedPR marks which PR is currently loaded in the diff/chat panels.
func (m *PRListModel) SetSelectedPR(number int) {
	*m.selectedPRNumber = number
}

// SetCIStatus updates the CI badge for the selected PR.
func (m *PRListModel) SetCIStatus(status string) {
	*m.ciOverallStatus = status
}

// SetReviewDecision updates the review badge for the selected PR.
func (m *PRListModel) SetReviewDecision(decision string) {
	*m.reviewDecision = decision
}

// ciBadgeForList returns a styled CI badge string and its visual width for the PR list.
func ciBadgeForList(status string) (string, int) {
	var icon, color string
	switch status {
	case "passing":
		icon, color = "✓", "42"
	case "failing":
		icon, color = "✗", "196"
	case "pending":
		icon, color = "●", "226"
	case "mixed":
		icon, color = "⚠", "208"
	default:
		return "", 0
	}
	styled := " " + lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon)
	return styled, 2
}

// reviewBadgeForList returns a styled review badge string and its visual width for the PR list.
func reviewBadgeForList(decision string) (string, int) {
	var icon, color string
	switch decision {
	case "APPROVED":
		icon, color = "✓", "76"
	case "CHANGES_REQUESTED":
		icon, color = "✗", "196"
	case "REVIEW_REQUIRED":
		icon, color = "○", "214"
	default:
		return "", 0
	}
	styled := " " + lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon)
	return styled, 2
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

// IsFiltering returns true when the user is actively typing in the filter input.
func (m PRListModel) IsFiltering() bool {
	return m.list.FilterState() == list.Filtering
}

// HasActiveFilter returns true when a filter is being typed or has been applied.
func (m PRListModel) HasActiveFilter() bool {
	fs := m.list.FilterState()
	return fs == list.Filtering || fs == list.FilterApplied
}

func (m PRListModel) Update(msg tea.Msg) (PRListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if m.state == stateLoading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case tea.KeyMsg:
		// While filtering, let the inner list handle all keys —
		// except Enter on empty input, which should clear the filter.
		if m.IsFiltering() {
			if msg.Type == tea.KeyEnter && m.list.FilterInput.Value() == "" {
				m.list.ResetFilter()
				return m, nil
			}
			break
		}
		switch {
		case key.Matches(msg, PRListKeys.PrevTab):
			if m.activeTab == TabMyPRs {
				m.activeTab = TabToReview
				m.list.ResetFilter()
				if m.state == stateLoaded {
					m.list.SetItems(m.toReview)
				}
			}
			return m, nil
		case key.Matches(msg, PRListKeys.NextTab):
			if m.activeTab == TabToReview {
				m.activeTab = TabMyPRs
				m.list.ResetFilter()
				if m.state == stateLoaded {
					m.list.SetItems(m.myPRs)
				}
			}
			return m, nil
		case key.Matches(msg, PRListKeys.SelectAndAdvance):
			if item, ok := m.list.SelectedItem().(PRItem); ok {
				return m, func() tea.Msg {
					return PRSelectedAndAdvanceMsg{
						Owner:   item.owner,
						Repo:    item.repo,
						Number:  item.number,
						HTMLURL: item.htmlURL,
					}
				}
			}
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
		if m.activeTabEmpty() {
			content = m.renderEmpty()
		} else {
			content = m.list.View()
		}
	}

	sections := []string{header}
	if m.HasActiveFilter() && !m.IsFiltering() {
		sections = append(sections, m.renderFilterBadge())
	}
	sections = append(sections, content)
	inner := lipgloss.JoinVertical(lipgloss.Left, sections...)

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

func (m PRListModel) renderFilterBadge() string {
	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("62")).
		Bold(true).
		Padding(0, 1).
		Render("FILTERED")
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Render(" [Esc]clear [/]edit")
	return badge + hint
}

func (m PRListModel) renderLoading() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Padding(1, 2).
		Render(m.spinner.View() + " Loading PRs...")
}

func (m PRListModel) renderError() string {
	return renderErrorWithHint(formatUserError(m.errMsg), "Press r to retry")
}

// activeTabEmpty returns true if the current tab has zero items after loading.
func (m PRListModel) activeTabEmpty() bool {
	switch m.activeTab {
	case TabToReview:
		return len(m.toReview) == 0
	case TabMyPRs:
		return len(m.myPRs) == 0
	}
	return false
}

func (m PRListModel) renderEmpty() string {
	switch m.activeTab {
	case TabToReview:
		return renderEmptyState("No PRs awaiting your review", "")
	case TabMyPRs:
		return renderEmptyState("You haven't opened any PRs", "")
	}
	return ""
}
