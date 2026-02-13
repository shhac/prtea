package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DiffViewerModel manages the diff viewer panel.
type DiffViewerModel struct {
	viewport viewport.Model
	width    int
	height   int
	focused  bool
	ready    bool
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
		m.viewport.SetContent(renderPlaceholderDiff(innerWidth))
		m.ready = true
	} else {
		m.viewport.Width = innerWidth
		m.viewport.Height = innerHeight
		m.viewport.SetContent(renderPlaceholderDiff(innerWidth))
	}
}

func (m *DiffViewerModel) SetFocused(focused bool) {
	m.focused = focused
}

func (m DiffViewerModel) View() string {
	header := panelHeaderStyle(m.focused).Render("Diff Viewer")

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

func renderPlaceholderDiff(width int) string {
	var b strings.Builder

	fileHeader := diffFileHeaderStyle.Render("src/auth/handler.go (+42/-8)")
	b.WriteString(fileHeader)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(width, 60)))
	b.WriteString("\n\n")

	hunk := diffHunkHeaderStyle.Render("@@ -10,6 +10,8 @@ package auth")
	b.WriteString(hunk)
	b.WriteString("\n")

	lines := []struct {
		prefix string
		text   string
	}{
		{" ", ` import (`},
		{" ", `     "context"`},
		{" ", `     "fmt"`},
		{"+", `     "time"`},
		{"+", `     "errors"`},
		{" ", ` )`},
		{" ", ``},
		{" ", ` func HandleLogin(ctx context.Context, req LoginRequest) (*Token, error) {`},
		{"-", `     token, err := authenticate(req.Username, req.Password)`},
		{"+", `     token, err := authenticateWithTimeout(ctx, req.Username, req.Password)`},
		{" ", `     if err != nil {`},
		{"-", `         return nil, err`},
		{"+", `         return nil, fmt.Errorf("authentication failed: %w", err)`},
		{" ", `     }`},
		{"+", ``},
		{"+", `     // Set token expiration`},
		{"+", `     token.ExpiresAt = time.Now().Add(24 * time.Hour)`},
		{" ", `     return token, nil`},
		{" ", ` }`},
	}

	for _, l := range lines {
		var line string
		switch l.prefix {
		case "+":
			line = diffAddedStyle.Render("+" + l.text)
		case "-":
			line = diffRemovedStyle.Render("-" + l.text)
		default:
			line = " " + l.text
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")

	fileHeader2 := diffFileHeaderStyle.Render("src/auth/timeout.go (new file)")
	b.WriteString(fileHeader2)
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", min(width, 60)))
	b.WriteString("\n\n")

	hunk2 := diffHunkHeaderStyle.Render("@@ -0,0 +1,25 @@ ")
	b.WriteString(hunk2)
	b.WriteString("\n")

	newFileLines := []string{
		`package auth`,
		``,
		`import (`,
		`    "context"`,
		`    "fmt"`,
		`    "time"`,
		`)`,
		``,
		`const defaultTimeout = 30 * time.Second`,
		``,
		`func authenticateWithTimeout(ctx context.Context, user, pass string) (*Token, error) {`,
		`    ctx, cancel := context.WithTimeout(ctx, defaultTimeout)`,
		`    defer cancel()`,
		``,
		`    result := make(chan authResult, 1)`,
		`    go func() {`,
		`        token, err := authenticate(user, pass)`,
		`        result <- authResult{token: token, err: err}`,
		`    }()`,
		``,
		`    select {`,
		`    case r := <-result:`,
		`        return r.token, r.err`,
		`    case <-ctx.Done():`,
		`        return nil, fmt.Errorf("authentication timed out")`,
		`    }`,
		`}`,
	}

	for _, l := range newFileLines {
		b.WriteString(diffAddedStyle.Render("+" + l))
		b.WriteString("\n")
	}

	return b.String()
}
