package tmux

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Past is an earlier Claude Code conversation in a directory, to resume.
type Past struct {
	ID    string `json:"id"` // session id: the transcript's file name
	Title string `json:"title"`
	Last  string `json:"last,omitempty"`
	At    int64  `json:"at"` // unix seconds of the last write
	Size  int64  `json:"size"`
	Open  string `json:"open,omitempty"` // tmux session showing it now (hooks only)
}

// SessionID is what Claude Code names a conversation (a UUID).
var SessionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type pastCache struct {
	mod  time.Time
	size int64
	sum  *Summary
}

var (
	histMu  sync.Mutex
	histSum = map[string]pastCache{}
)

// ProjectDir is where Claude Code keeps the transcripts of conversations
// started in cwd.
func ProjectDir(projects, cwd string) string {
	return filepath.Join(projects, nonAlnum.ReplaceAllString(cwd, "-"))
}

// History lists the newest n conversations started in cwd that have a
// prompt in them. Summaries are cached by size and mtime: each costs a
// tail read of the transcript.
func History(projects, cwd string, n int) []Past {
	dir := ProjectDir(projects, cwd)
	ents, _ := os.ReadDir(dir)
	type file struct {
		id   string
		info os.FileInfo
	}
	var files []file
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") || !SessionID.MatchString(id) {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.Size() > 0 {
			files = append(files, file{id, fi})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].info.ModTime().After(files[j].info.ModTime()) })
	out := []Past{}
	histMu.Lock()
	defer histMu.Unlock()
	for _, f := range files {
		if len(out) >= n {
			break
		}
		p := filepath.Join(dir, f.id+".jsonl")
		c, ok := histSum[p]
		if !ok || !c.mod.Equal(f.info.ModTime()) || c.size != f.info.Size() {
			_, sum := ReadTranscript(p)
			c = pastCache{f.info.ModTime(), f.info.Size(), sum}
			histSum[p] = c
		}
		if c.sum == nil || c.sum.Title == "" {
			continue // opened and left without a prompt
		}
		out = append(out, Past{ID: f.id, Title: c.sum.Title, Last: c.sum.Last, At: f.info.ModTime().Unix(), Size: f.info.Size()})
	}
	if len(histSum) > 500 { // a crude bound; summaries are re-read when needed
		clear(histSum)
	}
	return out
}

// HasConversation reports whether id is a conversation started in cwd.
func HasConversation(projects, cwd, id string) bool {
	if !SessionID.MatchString(id) {
		return false
	}
	fi, err := os.Stat(filepath.Join(ProjectDir(projects, cwd), id+".jsonl"))
	return err == nil && fi.Mode().IsRegular()
}

// OpenTranscripts maps transcripts known from hooks to the session showing
// them. Without hooks the transcript is only a guess, so it is left out.
func (m *Monitor) OpenTranscripts() map[string]string {
	out := map[string]string{}
	if m.Hooks == nil {
		return out
	}
	m.mu.Lock()
	panes := map[string]string{}
	for _, s := range m.sessions {
		panes[s.PaneID] = s.Name
	}
	m.mu.Unlock()
	for pane, name := range panes {
		if t := m.Hooks.transcript(pane); t != "" {
			out[t] = name
		}
	}
	return out
}
