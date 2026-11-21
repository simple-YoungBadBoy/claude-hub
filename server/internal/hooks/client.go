// Package hooks connects Claude Code hooks to the hub: the `claude-hub hook`
// client that Claude Code runs on every event, and the installer that adds
// it to ~/.claude/settings.json.
package hooks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claudehub/internal/tmux"
)

// Input is the subset of the hook payload we read from stdin.
type Input struct {
	Event      string `json:"hook_event_name"`
	Session    string `json:"session_id"`
	Transcript string `json:"transcript_path"`
	Tool       string `json:"tool_name"`
	NType      string `json:"notification_type"`
	Message    string `json:"message"`
	Error      string `json:"error"`
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
}

// Event turns a hook payload into what the hub needs. Prompts, tool inputs
// and outputs are never forwarded.
func Event(pane string, in Input) tmux.HookEvent {
	msg := in.Message
	if msg == "" {
		msg = in.Error
	}
	return tmux.HookEvent{
		Pane: pane, Event: in.Event, Session: in.Session, Transcript: in.Transcript,
		Tool: clip(in.Tool, 60), NType: clip(in.NType, 40), Message: clip(msg, 120),
	}
}

// DefaultDataDir is <binary dir>/../data, matching the install layout.
func DefaultDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Join(filepath.Dir(exe), "..", "data")
}

// RunClient is `claude-hub hook`. It must never disturb Claude Code: no
// output, always exit 0, done within about a second.
func RunClient() {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return // not inside tmux: nothing to map the event to
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	if err != nil {
		return
	}
	var in Input
	if json.Unmarshal(raw, &in) != nil || in.Event == "" {
		return
	}
	data := os.Getenv("CLAUDE_HUB_DATA")
	if data == "" {
		data = DefaultDataDir()
	}
	tok, err := os.ReadFile(filepath.Join(data, "token"))
	if err != nil {
		return
	}
	url := os.Getenv("CLAUDE_HUB_URL")
	if url == "" {
		url = "http://127.0.0.1:7788"
	}
	body, _ := json.Marshal(Event(pane, in))
	req, err := http.NewRequest("POST", strings.TrimRight(url, "/")+"/api/hook", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	req.Header.Set("X-Hub", "1")
	req.Header.Set("Content-Type", "application/json")
	c := http.Client{Timeout: 800 * time.Millisecond}
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
}
