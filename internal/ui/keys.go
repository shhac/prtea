package ui

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap defines keys available in navigation mode regardless of focused panel.
type GlobalKeyMap struct {
	Quit         key.Binding
	Help         key.Binding
	Tab          key.Binding
	ShiftTab     key.Binding
	Panel1       key.Binding
	Panel2       key.Binding
	Panel3       key.Binding
	Analyze      key.Binding
	OpenBrowser  key.Binding
	Refresh      key.Binding
	ToggleLeft   key.Binding
	ToggleCenter key.Binding
	ToggleRight  key.Binding
	Zoom         key.Binding
	CommandMode  key.Binding
	ExCommand    key.Binding
	ReviewPanel  key.Binding
}

var GlobalKeys = GlobalKeyMap{
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("Tab", "next panel"),
	),
	ShiftTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("Shift+Tab", "prev panel"),
	),
	Panel1: key.NewBinding(
		key.WithKeys("1"),
		key.WithHelp("1", "PR list"),
	),
	Panel2: key.NewBinding(
		key.WithKeys("2"),
		key.WithHelp("2", "diff viewer"),
	),
	Panel3: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "chat panel"),
	),
	Analyze: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "analyze"),
	),
	OpenBrowser: key.NewBinding(
		key.WithKeys("o"),
		key.WithHelp("o", "open in browser"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	ToggleLeft: key.NewBinding(
		key.WithKeys("["),
		key.WithHelp("[", "toggle left panel"),
	),
	ToggleCenter: key.NewBinding(
		key.WithKeys("\\"),
		key.WithHelp("\\", "toggle center panel"),
	),
	ToggleRight: key.NewBinding(
		key.WithKeys("]"),
		key.WithHelp("]", "toggle right panel"),
	),
	Zoom: key.NewBinding(
		key.WithKeys("z"),
		key.WithHelp("z", "zoom panel"),
	),
	CommandMode: key.NewBinding(
		key.WithKeys("ctrl+p"),
		key.WithHelp("Ctrl+P", "quick palette"),
	),
	ExCommand: key.NewBinding(
		key.WithKeys(":"),
		key.WithHelp(":", "command mode"),
	),
	ReviewPanel: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("Ctrl+R", "review tab"),
	),
}

// PRListKeyMap defines keys for the PR list panel.
type PRListKeyMap struct {
	Up               key.Binding
	Down             key.Binding
	Select           key.Binding
	SelectAndAdvance key.Binding
	PrevTab          key.Binding
	NextTab          key.Binding
}

var PRListKeys = PRListKeyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("Space", "select"),
	),
	SelectAndAdvance: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("Enter", "select + focus diff"),
	),
	PrevTab: key.NewBinding(
		key.WithKeys("h", "left"),
		key.WithHelp("h", "prev tab"),
	),
	NextTab: key.NewBinding(
		key.WithKeys("l", "right"),
		key.WithHelp("l", "next tab"),
	),
}

// DiffViewerKeyMap defines keys for the diff viewer panel.
type DiffViewerKeyMap struct {
	Up                    key.Binding
	Down                  key.Binding
	SelectDown            key.Binding
	SelectUp              key.Binding
	HalfDown              key.Binding
	HalfUp                key.Binding
	NextHunk              key.Binding
	PrevHunk              key.Binding
	Top                   key.Binding
	Bottom                key.Binding
	PrevTab               key.Binding
	NextTab               key.Binding
	SelectHunk            key.Binding
	SelectHunkAndAdvance  key.Binding
	SelectFileHunks       key.Binding
	ClearSelection        key.Binding
	Search                key.Binding
	RerunCI               key.Binding
}

var DiffViewerKeys = DiffViewerKeyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j", "down"),
	),
	SelectDown: key.NewBinding(
		key.WithKeys("J", "shift+down"),
		key.WithHelp("J", "extend selection down"),
	),
	SelectUp: key.NewBinding(
		key.WithKeys("K", "shift+up"),
		key.WithHelp("K", "extend selection up"),
	),
	HalfDown: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("Ctrl+d", "half page down"),
	),
	HalfUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("Ctrl+u", "half page up"),
	),
	NextHunk: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "next hunk"),
	),
	PrevHunk: key.NewBinding(
		key.WithKeys("N"),
		key.WithHelp("N", "prev hunk"),
	),
	Top: key.NewBinding(
		key.WithKeys("g"),
		key.WithHelp("g", "top"),
	),
	Bottom: key.NewBinding(
		key.WithKeys("G"),
		key.WithHelp("G", "bottom"),
	),
	PrevTab: key.NewBinding(
		key.WithKeys("h", "left"),
		key.WithHelp("h", "prev tab"),
	),
	NextTab: key.NewBinding(
		key.WithKeys("l", "right"),
		key.WithHelp("l", "next tab"),
	),
	SelectHunk: key.NewBinding(
		key.WithKeys("s", " "),
		key.WithHelp("s/Space", "select hunk"),
	),
	SelectHunkAndAdvance: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("Enter", "select hunk + focus chat"),
	),
	SelectFileHunks: key.NewBinding(
		key.WithKeys("S"),
		key.WithHelp("S", "select file hunks"),
	),
	ClearSelection: key.NewBinding(
		key.WithKeys(),
		key.WithHelp("", "clear selection"),
		key.WithDisabled(),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	RerunCI: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "re-run failed CI"),
	),
}

// ChatKeyMap defines keys for the chat panel.
type ChatKeyMap struct {
	Up         key.Binding
	Down       key.Binding
	ExitInsert key.Binding
	Send       key.Binding
	PrevTab    key.Binding
	NextTab    key.Binding
	NewChat    key.Binding
}

var ChatKeys = ChatKeyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j", "down"),
	),
	ExitInsert: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("Esc", "normal mode"),
	),
	Send: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("Enter", "send"),
	),
	PrevTab: key.NewBinding(
		key.WithKeys("h", "left"),
		key.WithHelp("h", "prev tab"),
	),
	NextTab: key.NewBinding(
		key.WithKeys("l", "right"),
		key.WithHelp("l", "next tab"),
	),
	NewChat: key.NewBinding(
		key.WithKeys("C"),
		key.WithHelp("C", "new chat"),
	),
}
