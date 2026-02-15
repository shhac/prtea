package notify

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Send delivers an OS-level notification with the given title and body.
// On macOS it uses osascript, on Linux notify-send, with a terminal bell fallback.
// Errors are returned but callers may choose to ignore them (fire-and-forget).
func Send(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return sendDarwin(title, body)
	case "linux":
		return sendLinux(title, body)
	default:
		return sendBell()
	}
}

// sendDarwin uses osascript to display a macOS notification.
func sendDarwin(title, body string) error {
	script := fmt.Sprintf(
		`display notification %s with title %s`,
		escapeAppleScript(body),
		escapeAppleScript(title),
	)
	return exec.Command("osascript", "-e", script).Run()
}

// sendLinux uses notify-send to display a desktop notification.
func sendLinux(title, body string) error {
	return exec.Command("notify-send", "-a", "prtea", title, body).Run()
}

// sendBell writes a terminal bell character as a lightweight fallback.
func sendBell() error {
	_, err := fmt.Print("\a")
	return err
}

// escapeAppleScript returns a quoted AppleScript string with internal
// quotes and backslashes escaped.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
