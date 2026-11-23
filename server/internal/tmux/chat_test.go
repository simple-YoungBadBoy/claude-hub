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
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"1 import x\n2 export y"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"npm test"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","is_error":true,"content":[{"type":"text","text":"fail"},{"type":"image"}]}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"**改好了**"}]}}`,
		`{"type":"user","isMeta":true,"message":{"content":"meta"}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"谢谢"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Task","input":{"description":"查资料"}}]}}`,
		`not json`,
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	c, err := ReadChat(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	m := c.Messages
	if len(m) != 4 || m[0].Role != "user" || m[0].Text != "把按钮改成蓝色" || m[0].At == "" {
		t.Fatalf("%+v", m)
	}
	a := m[1]
	if a.Role != "assistant" || a.Text != "好的，先看看。\n\n**改好了**" || len(a.Tools) != 2 {
		t.Fatalf("assistant records merge: %+v", a)
	}
	if a.Tools[0] != (ChatTool{Name: "Read", Input: "/p/a.tsx", Output: "1 import x\n2 export y"}) || !a.Tools[1].Error || a.Tools[1].Input != "npm test" ||
		a.Tools[1].Output != "fail\n[图片]" || a.Tools[1].Running {
		t.Fatalf("tools %+v", a.Tools)
	}
	if r := m[3].Tools; len(r) != 1 || !r[0].Running || r[0].Input != "查资料" {
		t.Fatalf("a call without a result yet is running: %+v", m[3])
	}
	if m[2].Text != "谢谢" {
		t.Fatalf("%+v", m[2])
	}
	c, _ = ReadChat(p, 2)
	if !c.Truncated || len(c.Messages) != 2 || c.Messages[0].Role != "user" {
		t.Fatalf("max keeps the newest: %+v", c)
	}
	if _, err := ReadChat(filepath.Join(t.TempDir(), "none.jsonl"), 10); err == nil {
		t.Fatal("missing file")
	}
}

func TestClipStripsMarkupNoise(t *testing.T) {
	got := clip("<div align=\"center\">\n## 改好了\n- 加了 **一行**\n\n| 文件 | 行 |\n|---|---|\n| a.md | 2 |", 80)
	if got != "改好了 - 加了 一行 文件 行 a.md 2" {
		t.Fatalf("%q", got)
	}
	if got := clip("a < b and c > d", 80); got != "a < b and c > d" {
		t.Fatalf("plain comparisons kept: %q", got)
	}
}

func TestResultTextClips(t *testing.T) {
	long := strings.Repeat("行\n", 100)
	got := resultText([]byte(`"` + strings.ReplaceAll(long, "\n", `\n`) + `"`))
	if strings.Count(got, "\n") != outputLines || !strings.HasSuffix(got, "…") {
		t.Fatalf("%q", got)
	}
	if got := resultText([]byte(`"` + strings.Repeat("字", 5000) + `"`)); len([]rune(got)) != outputRunes+2 {
		t.Fatalf("%d", len([]rune(got)))
	}
}
