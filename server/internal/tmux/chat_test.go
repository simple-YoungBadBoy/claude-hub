package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadChat(t *testing.T) {
	lines := []string{
		`{"type":"ai-title","aiTitle":"x"}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","timestamp":"2026-11-23T01:00:00Z","message":{"role":"user","content":"把按钮改成蓝色"}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"好的，先看看。"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/p/a.tsx"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"..."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"npm test"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":"fail"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"**改好了**"}]}}`,
		`{"type":"user","isMeta":true,"message":{"content":"meta"}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"谢谢"}]}}`,
		`not json`,
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	c, err := ReadChat(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	m := c.Messages
	if len(m) != 3 || m[0].Role != "user" || m[0].Text != "把按钮改成蓝色" || m[0].At == "" {
		t.Fatalf("%+v", m)
	}
	a := m[1]
	if a.Role != "assistant" || a.Text != "好的，先看看。\n\n**改好了**" || len(a.Tools) != 2 {
		t.Fatalf("assistant records merge: %+v", a)
	}
	if a.Tools[0] != (ChatTool{Name: "Read", Input: "/p/a.tsx"}) || !a.Tools[1].Error || a.Tools[1].Input != "npm test" {
		t.Fatalf("tools %+v", a.Tools)
	}
	if m[2].Text != "谢谢" {
		t.Fatalf("%+v", m[2])
	}
	c, _ = ReadChat(p, 2)
	if !c.Truncated || len(c.Messages) != 2 || c.Messages[0].Role != "assistant" {
		t.Fatalf("max keeps the newest: %+v", c)
	}
	if _, err := ReadChat(filepath.Join(t.TempDir(), "none.jsonl"), 10); err == nil {
		t.Fatal("missing file")
	}
}
