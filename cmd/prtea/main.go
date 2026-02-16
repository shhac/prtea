package main

import (
	"fmt"
	"os"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shhac/prtea/internal/ui"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var opts []ui.AppOption

	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "version":
			fmt.Printf("prtea %s (commit: %s, built: %s)\n", version, commit, date)
			os.Exit(0)
		case "-v":
			fmt.Printf("prtea %s\n", version)
			fmt.Printf("  commit: %s\n", commit)
			fmt.Printf("  built:  %s\n", date)
			fmt.Printf("  go:     %s\n", runtime.Version())
			fmt.Printf("  os:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
			os.Exit(0)
		case "--demo":
			opts = append(opts, ui.WithDemo())
		}
	}

	p := tea.NewProgram(ui.NewApp(opts...), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
