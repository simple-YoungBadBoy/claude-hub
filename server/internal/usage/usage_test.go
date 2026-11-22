package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func asst(ts time.Time, id, model, cwd string, in, out, cr, cw int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"requestId":"req_%s","cwd":%q,"message":{"id":"msg_%s","model":%q,"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`+"\n",
		ts.UTC().Format(time.RFC3339Nano), id, cwd, id, model, in, out, cr, cw)
}

func write(t *testing.T, path, s string, appendTo bool) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o700)
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendTo {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

func TestReport(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 22, 15, 0, 0, 0, time.Local)
	yesterday := now.AddDate(0, 0, -1)
	old := now.AddDate(0, 0, -30)
	s := New(root)
	s.Now = func() time.Time { return now }
	s.MinEvery = 0

	main := filepath.Join(root, "-p1", "s1.jsonl")
	write(t, main,
		`{"type":"user","message":{"content":"hi"}}`+"\n"+
			asst(now, "a", "claude-opus-5-5", "/p1", 10, 100, 1000, 50)+
			asst(now, "a", "claude-opus-5-5", "/p1", 10, 100, 1000, 50)+ // same response, 2nd content block
			asst(now, "b", "claude-haiku-5-5", "/p1", 1, 2, 3, 4)+
			asst(yesterday, "c", "claude-opus-5-5", "/p1", 7, 7, 7, 7)+
			asst(old, "d", "claude-opus-5-5", "/p1", 999, 999, 999, 999)+
			`{"type":"assistant","timestamp":"`+now.UTC().Format(time.RFC3339)+`","message":{"id":"msg_e","model":"<synthetic>","usage":{"input_tokens":0,"output_tokens":0}}}`+"\n", false)
	write(t, filepath.Join(root, "-p2", "s2", "subagents", "agent-1.jsonl"),
		asst(now, "s", "claude-haiku-5-5", "/p2", 5, 5, 5, 5), false)

	r := s.Report()
	want := Counts{Input: 16, Output: 107, CacheRead: 1008, CacheWrite: 59, Messages: 3}
	if r.Today != want {
		t.Fatalf("today %+v, want %+v", r.Today, want)
	}
	if r.TodaySub != (Counts{5, 5, 5, 5, 1}) {
		t.Fatalf("sub %+v", r.TodaySub)
	}
	if len(r.Days) != 7 || r.Days[6].Day != "2026-11-22" || r.Days[5].Input != 7 || r.Days[0].Day != "2026-11-16" {
		t.Fatalf("days %+v", r.Days)
	}
	if len(r.Models) != 2 || r.Models[0].Name != "claude-opus-5-5" || r.Models[0].Messages != 1 {
		t.Fatalf("models %+v", r.Models)
	}
	if len(r.Projects) != 2 || r.Projects[0].Name != "/p1" || r.Projects[1].Name != "/p2" {
		t.Fatalf("projects %+v", r.Projects)
	}

	// incremental: a partial line is not counted until it is complete
	line := asst(now, "f", "claude-opus-5-5", "/p1", 1, 1, 1, 1)
	write(t, main, line[:40], true)
	if r := s.Report(); r.Today.Messages != 3 {
		t.Fatalf("partial line counted: %+v", r.Today)
	}
	write(t, main, line[40:], true)
	if r := s.Report(); r.Today.Messages != 4 || r.Today.Input != 17 {
		t.Fatalf("appended line not counted once: %+v", r.Today)
	}
	if r := s.Report(); r.Today.Messages != 4 {
		t.Fatalf("rescan double counted: %+v", r.Today)
	}
}

func TestSkipsHugeLinesAndKeepsGoing(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	s := New(root)
	huge := `{"type":"user","message":{"content":"` + strings.Repeat("x", maxLine+10) + `"}}` + "\n"
	write(t, filepath.Join(root, "p", "s.jsonl"), huge+asst(now, "a", "m", "/p", 1, 2, 3, 4), false)
	if r := s.Report(); r.Today.Messages != 1 || r.Today.Output != 2 {
		t.Fatalf("got %+v", r.Today)
	}
}

func TestDayRollover(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 22, 23, 0, 0, 0, time.Local)
	s := New(root)
	s.MinEvery = 0
	s.Now = func() time.Time { return now }
	write(t, filepath.Join(root, "p", "s.jsonl"), asst(now, "a", "m", "/p", 1, 1, 1, 1), false)
	if r := s.Report(); r.Today.Messages != 1 {
		t.Fatal(r.Today)
	}
	now = now.Add(2 * time.Hour) // next day
	r := s.Report()
	if r.Today.Messages != 0 || r.Days[5].Messages != 1 || len(r.Models) != 0 {
		t.Fatalf("after midnight: %+v", r)
	}
}

func TestMissingRoot(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nope"))
	if r := s.Report(); r.Today.Messages != 0 || len(r.Days) != 7 {
		t.Fatalf("%+v", r)
	}
}
