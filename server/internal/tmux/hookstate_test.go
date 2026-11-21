package tmux

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHookStates(t *testing.T) {
	h := NewHookTracker()
	if _, _, _, ok := h.resolve("%1", "idle"); ok {
		t.Fatal("no hooks yet must fall back to screen")
	}
	steps := []struct {
		ev     HookEvent
		status string
		act    string
	}{
		{HookEvent{Event: "SessionStart"}, "idle", ""},
		{HookEvent{Event: "UserPromptSubmit"}, "working", "思考中"},
		{HookEvent{Event: "PreToolUse", Tool: "Bash"}, "working", "运行 Bash"},
		{HookEvent{Event: "PermissionRequest", Tool: "Bash"}, "waiting", "等你确认：Bash"},
		{HookEvent{Event: "PostToolUse"}, "working", "思考中"},
		{HookEvent{Event: "Notification", NType: "idle_prompt", Message: "waiting for input"}, "working", "思考中"},
		{HookEvent{Event: "Stop"}, "idle", ""},
		{HookEvent{Event: "StopFailure", Message: "rate limited"}, "error", "出错：rate limited"},
		{HookEvent{Event: "UserPromptSubmit"}, "working", "思考中"},
	}
	for i, s := range steps {
		s.ev.Pane = "%1"
		h.Apply(s.ev)
		screen := map[string]string{"error": "idle"}[s.status] // what Claude shows meanwhile
		if screen == "" {
			screen = s.status
		}
		st, act, _, ok := h.resolve("%1", screen)
		if !ok || st != s.status || act != s.act {
			t.Fatalf("step %d %s: got %q %q, want %q %q", i, s.ev.Event, st, act, s.status, s.act)
		}
	}
}

func TestEscInterruptFallsBackToScreen(t *testing.T) {
	h := NewHookTracker()
	h.Apply(HookEvent{Pane: "%2", Event: "UserPromptSubmit"})
	h.panes["%2"].at = time.Now().Add(-10 * time.Second) // turn started a while ago
	// user pressed Esc: no Stop hook, the screen is idle
	var st string
	for i := 0; i < 3; i++ {
		st, _, _, _ = h.resolve("%2", "idle")
	}
	if st != "idle" {
		t.Fatalf("stuck in %q after interrupt", st)
	}
}

func TestReadContext(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	lines := `{"type":"user","message":{"content":"hi"}}
{"type":"assistant","message":{"model":"claude-opus-5-5","usage":{"input_tokens":10,"cache_read_input_tokens":150000,"cache_creation_input_tokens":9990,"output_tokens":0}}}
{"type":"assistant","isSidechain":true,"message":{"usage":{"input_tokens":999999}}}
`
	os.WriteFile(p, []byte(lines), 0o600)
	c := ReadContext(p)
	if c == nil || c.Tokens != 160000 || c.Limit != 200000 || c.Pct != 80 {
		t.Fatalf("got %+v", c)
	}
	os.WriteFile(p, []byte(`{"type":"assistant","message":{"usage":{"input_tokens":5,"cache_read_input_tokens":300000}}}`+"\n"), 0o600)
	if c := ReadContext(p); c.Limit != 1_000_000 {
		t.Fatalf("1M window not inferred: %+v", c)
	}
}

func TestPruneForgetsDeadPanes(t *testing.T) {
	h := NewHookTracker()
	h.Apply(HookEvent{Pane: "%1", Event: "UserPromptSubmit"})
	h.Apply(HookEvent{Pane: "%2", Event: "UserPromptSubmit"})
	h.prune(map[string]bool{"%2": true})
	if _, _, _, ok := h.resolve("%1", "idle"); ok {
		t.Fatal("%1 should be forgotten")
	}
	if _, _, _, ok := h.resolve("%2", "working"); !ok {
		t.Fatal("%2 should be kept")
	}
}
