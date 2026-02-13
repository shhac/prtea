package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shhac/prtea/internal/github"
)

// DiffViewerTab identifies which sub-tab is active in the diff viewer.
type DiffViewerTab int

const (
	TabDiff   DiffViewerTab = iota
	TabPRInfo
)

// DiffViewerModel manages the diff viewer panel.
type DiffViewerModel struct {
	viewport  viewport.Model
	activeTab DiffViewerTab
	width     int
	height    int
	focused   bool
	ready     bool

	// Diff data
	files          []github.PRFile
	fileOffsets    []int // viewport line index where each file header starts
	currentFileIdx int
	loading        bool
	prNumber       int
	err            error

	// PR info data (for PR Info tab)
	prTitle  string
	prBody   string
	prAuthor string
	prURL    string
}

func NewDiffViewerModel() DiffViewerModel {
	return DiffViewerModel{}
}

func (m DiffViewerModel) Update(msg tea.Msg) (DiffViewerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}
		switch {
		case key.Matches(msg, DiffViewerKeys.PrevTab):
			if m.activeTab > TabDiff {
				m.activeTab--
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.NextTab):
			if m.activeTab < TabPRInfo {
				m.activeTab++
				m.refreshContent()
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.NextFile):
			if len(m.fileOffsets) > 0 {
				currentY := m.viewport.YOffset
				for i, offset := range m.fileOffsets {
					if offset > currentY {
						m.currentFileIdx = i
						m.viewport.SetYOffset(offset)
						break
					}
				}
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.PrevFile):
			if len(m.fileOffsets) > 0 {
				currentY := m.viewport.YOffset
				for i := len(m.fileOffsets) - 1; i >= 0; i-- {
					if m.fileOffsets[i] < currentY {
						m.currentFileIdx = i
						m.viewport.SetYOffset(m.fileOffsets[i])
						break
					}
				}
			}
			return m, nil
		case key.Matches(msg, DiffViewerKeys.HalfDown):
			m.viewport.HalfViewDown()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.HalfUp):
			m.viewport.HalfViewUp()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Top):
			m.viewport.GotoTop()
			return m, nil
		case key.Matches(msg, DiffViewerKeys.Bottom):
			m.viewport.GotoBottom()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *DiffViewerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	// Account for borders (2) and header (1)
	innerWidth := width - 4
	innerHeight := height - 5
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	if !m.ready {
		m.viewport = viewport.New(innerWidth, innerHeight)
		m.ready = true
	} else {
		m.viewport.Width = innerWidth
		m.viewport.Height = innerHeight
	}
	m.refreshContent()
}

func (m *DiffViewerModel) SetFocused(focused bool) {
	m.focused = focused
}

// SetLoading puts the viewer into loading state for a given PR.
func (m *DiffViewerModel) SetLoading(prNumber int) {
	m.prNumber = prNumber
	m.loading = true
	m.files = nil
	m.fileOffsets = nil
	m.currentFileIdx = 0
	m.err = nil
	m.refreshContent()
}

// SetDiff displays the fetched diff files.
func (m *DiffViewerModel) SetDiff(files []github.PRFile) {
	m.loading = false
	m.files = files
	m.err = nil
	m.currentFileIdx = 0
	m.refreshContent()
	m.viewport.GotoTop()
}

// SetError displays an error message.
func (m *DiffViewerModel) SetError(err error) {
	m.loading = false
	m.err = err
	m.files = nil
	m.fileOffsets = nil
	m.refreshContent()
}

// SetPRInfo sets PR metadata for the PR Info tab.
func (m *DiffViewerModel) SetPRInfo(title, body, author, url string) {
	m.prTitle = title
	m.prBody = body
	m.prAuthor = author
	m.prURL = url
	m.refreshContent()
}

func (m *DiffViewerModel) refreshContent() {
	if !m.ready {
		return
	}

	// PR Info tab has its own content path
	if m.activeTab == TabPRInfo {
		m.viewport.SetContent(m.renderPRInfo())
		return
	}

	// Diff tab
	if m.loading {
		m.viewport.SetContent(
			lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Padding(1, 2).
				Render(fmt.Sprintf("Loading diff for PR #%d...", m.prNumber)),
		)
		return
	}
	if m.err != nil {
		errMsg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Padding(1, 2).
			Render(fmt.Sprintf("Error: %v", m.err))
		hint := lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(0, 2).
			Render("Select a PR to try again")
		m.viewport.SetContent(lipgloss.JoinVertical(lipgloss.Left, errMsg, hint))
		return
	}
	if m.files != nil {
		m.viewport.SetContent(m.renderRealDiff())
		return
	}
	// No PR selected yet
	m.viewport.SetContent(
		lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render("Select a PR to view its diff"),
	)
}

func (m DiffViewerModel) View() string {
	header := m.renderTabs()

	var content string
	if m.ready {
		content = m.viewport.View()
	} else {
		content = "Loading..."
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, header, content)
	style := panelStyle(m.focused, false, m.width-2, m.height-2)
	return style.Render(inner)
}

func (m DiffViewerModel) renderTabs() string {
	var tabs []string

	diffLabel := "Diff"
	if m.prNumber > 0 && m.files != nil {
		diffLabel = fmt.Sprintf("Diff (%d files)", len(m.files))
	}
	prInfoLabel := "PR Info"

	tabNames := []struct {
		tab   DiffViewerTab
		label string
	}{
		{TabDiff, diffLabel},
		{TabPRInfo, prInfoLabel},
	}

	for _, t := range tabNames {
		if m.activeTab == t.tab {
			tabs = append(tabs, activeTabStyle().Render(t.label))
		} else {
			tabs = append(tabs, inactiveTabStyle().Render(t.label))
		}
	}

	return strings.Join(tabs, " ")
}

func (m DiffViewerModel) renderPRInfo() string {
	if m.prNumber == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render("Select a PR to view its details")
	}

	if m.prTitle == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render(fmt.Sprintf("Loading PR #%d info...", m.prNumber))
	}

	innerWidth := m.viewport.Width
	if innerWidth < 10 {
		innerWidth = 10
	}

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var b strings.Builder

	// Title
	b.WriteString(sectionStyle.Render(fmt.Sprintf("PR #%d", m.prNumber)))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(m.prTitle))
	b.WriteString("\n\n")

	// Author
	b.WriteString(dimStyle.Render("Author: "))
	b.WriteString(m.prAuthor)
	b.WriteString("\n")

	// URL
	if m.prURL != "" {
		b.WriteString(dimStyle.Render("URL: "))
		b.WriteString(m.prURL)
		b.WriteString("\n")
	}

	// Description
	if m.prBody != "" {
		b.WriteString("\n")
		b.WriteString(sectionStyle.Render("Description"))
		b.WriteString("\n")
		b.WriteString(wordWrap(m.prBody, innerWidth))
	} else {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("No description provided."))
	}

	return b.String()
}

// renderRealDiff renders actual PR file diffs with syntax coloring.
func (m *DiffViewerModel) renderRealDiff() string {
	if len(m.files) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Padding(1, 2).
			Render("No files changed in this PR.")
	}

	innerWidth := m.viewport.Width
	var b strings.Builder
	m.fileOffsets = make([]int, len(m.files))
	lineCount := 0

	for i, f := range m.files {
		if i > 0 {
			b.WriteString("\n")
			lineCount++
		}

		// Track line offset where this file header starts
		m.fileOffsets[i] = lineCount

		// File header
		b.WriteString(diffFileHeaderStyle.Render(fileStatusLabel(f)))
		b.WriteString("\n")
		lineCount++

		// Separator
		b.WriteString(strings.Repeat("─", min(innerWidth, 60)))
		b.WriteString("\n")
		lineCount++

		// Patch content
		if f.Patch == "" {
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("  (diff not available)"))
			b.WriteString("\n")
			lineCount++
			continue
		}

		b.WriteString("\n")
		lineCount++

		patchLines := strings.Split(f.Patch, "\n")
		for _, line := range patchLines {
			if line == "" {
				b.WriteString("\n")
				lineCount++
				continue
			}
			switch {
			case strings.HasPrefix(line, "@@"):
				b.WriteString(diffHunkHeaderStyle.Render(line))
			case strings.HasPrefix(line, "+"):
				b.WriteString(diffAddedStyle.Render(line))
			case strings.HasPrefix(line, "-"):
				b.WriteString(diffRemovedStyle.Render(line))
			case strings.HasPrefix(line, `\`):
				b.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color("244")).
					Italic(true).
					Render(line))
			default:
				b.WriteString(line)
			}
			b.WriteString("\n")
			lineCount++
		}
	}

	return b.String()
}

func fileStatusLabel(f github.PRFile) string {
	switch f.Status {
	case "added":
		return fmt.Sprintf("%s (new file, +%d)", f.Filename, f.Additions)
	case "removed":
		return fmt.Sprintf("%s (deleted, -%d)", f.Filename, f.Deletions)
	case "renamed":
		return fmt.Sprintf("%s (renamed)", f.Filename)
	default:
		return fmt.Sprintf("%s (+%d/-%d)", f.Filename, f.Additions, f.Deletions)
	}
}
