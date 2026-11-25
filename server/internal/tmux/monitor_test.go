package tmux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNudgeCoalesces(t *testing.T) {
	m := NewMonitor()
	for i := 0; i < 10; i++ {
		m.Nudge() // never blocks
	}
	if len(m.kick) != 1 {
		t.Fatalf("pending nudges = %d, want 1", len(m.kick))
	}
}

// resolve reads the transcript outside the tracker lock; the result must
// still land on the pane, and a pane that switched sessions meanwhile must
// not get the old session's context.
func TestResolveReadsContextOutsideLock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"好"}],"usage":{"input_tokens":42}}}`+"\n"), 0o600)
	h := NewHookTracker()
	h.Apply(HookEvent{Pane: "%1", Event: "UserPromptSubmit", Session: "a", Transcript: p})
	done := make(chan struct{})
	go func() { // hook events keep flowing while polls resolve
		for i := 0; i < 200; i++ {
			h.Apply(HookEvent{Pane: "%1", Event: "PostToolUse"})
		}
		close(done)
	}()
	st, _, ctx, ok := h.resolve("%1", "working")
	<-done
	if !ok || st != "working" || ctx == nil || ctx.Tokens != 42 {
		t.Fatalf("resolve = %q %+v %v", st, ctx, ok)
	}
	if s := h.summary("%1"); s == nil || s.Last != "好" {
		t.Fatalf("summary %+v", s)
	}
	h.Apply(HookEvent{Pane: "%1", Event: "SessionStart", Session: "b", Transcript: p + ".new"})
	if _, _, ctx, _ := h.resolve("%1", "idle"); ctx != nil {
		t.Fatalf("new session kept the old context %+v", ctx)
	}
}
