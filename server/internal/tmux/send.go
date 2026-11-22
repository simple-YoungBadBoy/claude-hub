package tmux

import (
	"strings"
	"time"
	"unicode"
)

// Send types a message into Claude Code's input and submits it. The text
// goes first; Enter follows only once the text shows on screen, because an
// Enter that overtakes the text submits an empty or partial prompt.
// Multi-line text is a bracketed paste (keeps newlines; Enter inside would
// submit). A single line is typed literally, so "/compact" still runs as a
// command (a pasted slash command is not dispatched).
func Send(name, text string) error {
	text = strings.TrimRightFunc(text, unicode.IsSpace)
	if text == "" {
		_, err := run("send-keys", "-t", "="+name+":", "Enter")
		return err
	}
	if strings.Contains(text, "\n") {
		if err := Paste(name, text); err != nil {
			return err
		}
	} else if _, err := run("send-keys", "-t", "="+name+":", "-l", "--", text); err != nil {
		return err
	}
	for deadline := time.Now().Add(1500 * time.Millisecond); time.Now().Before(deadline); {
		time.Sleep(60 * time.Millisecond)
		if scr, err := Capture(name); err == nil && landed(scr, text) {
			break
		}
	}
	time.Sleep(30 * time.Millisecond) // let Claude Code finish its paste handling
	_, err := run("send-keys", "-t", "="+name+":", "Enter")
	return err
}

// landed reports whether the end of text is visible near the bottom of the
// screen (Claude Code wraps long input and folds big pastes).
func landed(screen, text string) bool {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	bottom := strings.Join(lines, "")
	if strings.Contains(bottom, "[Pasted text") {
		return true
	}
	squash := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '│' {
				return -1
			}
			return r
		}, s)
	}
	t := []rune(squash(text))
	if len(t) > 8 {
		t = t[len(t)-8:]
	}
	return strings.Contains(squash(bottom), string(t))
}

// FastEscape lowers tmux's escape-time from its 500ms default, which delays
// every Esc (interrupting Claude) by half a second. It's a runtime server
// option: nothing is written to the user's tmux.conf, and a value the user
// chose (anything but the default) is left alone. ms < 0 disables it.
func FastEscape(ms int) {
	if ms < 0 {
		return
	}
	out, err := run("show-options", "-gsv", "escape-time")
	if err != nil || strings.TrimSpace(out) != "500" {
		return
	}
	run("set-option", "-gs", "escape-time", itoa(int64(ms)))
}
