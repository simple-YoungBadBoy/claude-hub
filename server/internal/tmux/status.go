package tmux

import "strings"

// Status classifies a Claude Code screen:
//
//	working — the spinner line shows "esc to interrupt"
//	waiting — a permission / choice prompt is open
//	idle    — anything else
func Status(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > 25 {
		lines = lines[len(lines)-25:]
	}
	tail := strings.ToLower(strings.Join(lines, "\n"))
	switch {
	case strings.Contains(tail, "esc to interrupt"):
		return "working"
	case strings.Contains(tail, "do you want to"),
		strings.Contains(tail, "❯ 1. yes"),
		strings.Contains(tail, "esc to cancel"):
		return "waiting"
	default:
		return "idle"
	}
}
