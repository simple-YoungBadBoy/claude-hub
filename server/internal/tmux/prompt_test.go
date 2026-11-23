package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captured from Claude Code 2.1 (tmux capture-pane)
const permScreen = `❯ Use the Bash tool to run exactly: touch hello.txt

  Creating empty hello.txt file
  ⎿  $ touch hello.txt

────────────────────────────────────────────────────────────────
 Bash command
 Tip: auto mode handles these prompts for you — choose "switch to auto mode" below
 Create empty hello.txt file
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 touch hello.txt
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and always allow access to /tmp/permtest
      from this project
   3. Yes, and switch to auto mode · auto mode handles these prompts for you
   4. No

 Esc to cancel · Tab to amend
`

func TestParsePrompt(t *testing.T) {
	p := ParsePrompt(permScreen)
	if p == nil {
		t.Fatal("no prompt found")
	}
	if p.Question != "Do you want to proceed?" {
		t.Fatalf("question %q", p.Question)
	}
	if got := strings.Join(p.Detail, "|"); got != "Bash command|Create empty hello.txt file|touch hello.txt" {
		t.Fatalf("detail %q", got)
	}
	if len(p.Options) != 4 || p.Options[0] != (Option{"1", "Yes"}) || p.Options[3] != (Option{"4", "No"}) {
		t.Fatalf("options %+v", p.Options)
	}
	if p.Options[1].Label != "Yes, and always allow access to /tmp/permtest from this project" {
		t.Fatalf("wrapped label %q", p.Options[1].Label)
	}
}

func TestParsePromptBoxed(t *testing.T) {
	p := ParsePrompt("╭──────────────╮\n│ Bash command │\n│   rm foo     │\n│ Do you want to proceed? │\n│ ❯ 1. Yes     │\n│   2. No      │\n╰──────────────╯\n")
	if p == nil || p.Question != "Do you want to proceed?" || len(p.Options) != 2 || p.Options[1].Label != "No" || strings.Join(p.Detail, "|") != "Bash command|rm foo" {
		t.Fatalf("got %+v", p)
	}
}

func TestParsePromptIgnoresOldLists(t *testing.T) {
	// a numbered list in Claude's answer, with the input box below it
	scr := "Here is the plan:\n1. Read\n2. Write\n\nmore text\nand more\n────\n❯ \n────\n  ? for shortcuts\n"
	if p := ParsePrompt(scr); p != nil {
		t.Fatalf("not a prompt: %+v", p)
	}
}

func TestReadTranscriptSummary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	lines := `{"type":"user","message":{"role":"user","content":"修复登录页的 bug"}}
{"type":"ai-title","aiTitle":"Fix login page"}
{"type":"api-request-blob","message":{"content":"` + strings.Repeat("x", 100000) + `"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"改好了：` + "`login.ts`" + ` 里\n的判断写反了。"}],"usage":{"input_tokens":10}}}
`
	os.WriteFile(p, []byte(lines), 0o600)
	c, s := ReadTranscript(p)
	if s == nil || s.Title != "Fix login page" || s.Last != "改好了：login.ts 里 的判断写反了。" {
		t.Fatalf("summary %+v", s)
	}
	if c == nil || c.Tokens != 10 {
		t.Fatalf("context %+v", c)
	}
	// a new prompt makes the old reply stale
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}` + "\n")
	f.WriteString(`{"type":"user","message":{"content":"再加个测试"}}` + "\n")
	f.Close()
	if _, s := ReadTranscript(p); s.Last != "" || s.Title != "Fix login page" {
		t.Fatalf("after new prompt %+v", s)
	}
}

func TestLatestTranscript(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "-home-u-my-proj-x")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("{}"), 0o600)
	if got := LatestTranscript(root, "/home/u/my.proj_x"); got != filepath.Join(dir, "a.jsonl") {
		t.Fatalf("got %q", got)
	}
}

func TestHistory(t *testing.T) {
	root := t.TempDir()
	dir := ProjectDir(root, "/home/u/proj")
	os.MkdirAll(dir, 0o700)
	a, b, empty := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"
	os.WriteFile(filepath.Join(dir, a+".jsonl"), []byte(`{"type":"ai-title","aiTitle":"修登录"}`+"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, b+".jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"加个按钮"}}`+"\n"+`{"type":"assistant","message":{"content":[{"type":"text","text":"加好了"}]}}`+"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, empty+".jsonl"), []byte(`{"type":"summary"}`+"\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "notes.jsonl"), []byte(`{"type":"ai-title","aiTitle":"x"}`+"\n"), 0o600)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, a+".jsonl"), old, old)
	h := History(root, "/home/u/proj", 10)
	if len(h) != 2 || h[0].ID != b || h[0].Title != "加个按钮" || h[0].Last != "加好了" || h[1].Title != "修登录" {
		t.Fatalf("newest first, untitled and non-uuid skipped: %+v", h)
	}
	if h := History(root, "/home/u/proj", 1); len(h) != 1 {
		t.Fatalf("limit: %+v", h)
	}
	if !HasConversation(root, "/home/u/proj", a) || HasConversation(root, "/home/u/other", a) ||
		HasConversation(root, "/home/u/proj", "notes") || HasConversation(root, "/home/u/proj", "../"+a) {
		t.Fatal("HasConversation")
	}
}
