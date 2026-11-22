// Package usage totals Claude Code token usage from the transcripts under
// ~/.claude/projects (or $CLAUDE_CONFIG_DIR/projects): today by model and by
// project, plus a daily series. Files are read incrementally (only appended
// bytes), only files touched inside the window are opened, and lines that
// can't hold usage are skipped before JSON decoding.
package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Counts are token totals. Messages counts distinct API responses.
type Counts struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Messages   int64 `json:"messages"`
}

func (c *Counts) add(o Counts) {
	c.Input += o.Input
	c.Output += o.Output
	c.CacheRead += o.CacheRead
	c.CacheWrite += o.CacheWrite
	c.Messages += o.Messages
}

// Named is a breakdown row.
type Named struct {
	Name string `json:"name"`
	Counts
}

// Day is one entry of the daily series.
type Day struct {
	Day string `json:"day"` // YYYY-MM-DD, server local time
	Counts
}

// Report is what the UI shows.
type Report struct {
	Today     Counts  `json:"today"`
	TodaySub  Counts  `json:"todaySub"` // part of Today spent by subagents
	Models    []Named `json:"models"`   // today
	Projects  []Named `json:"projects"` // today
	Days      []Day   `json:"days"`     // oldest first, ends today
	ScannedAt int64   `json:"scannedAt"`
}

// maxLine bounds memory per line; longer lines (huge tool output) are skipped.
const maxLine = 4 << 20

type bucket struct {
	total, sub Counts
	models     map[string]*Counts
	projects   map[string]*Counts
}

func newBucket() *bucket {
	return &bucket{models: map[string]*Counts{}, projects: map[string]*Counts{}}
}

// Scanner keeps per-file offsets and per-day totals between scans.
type Scanner struct {
	Root     string        // the projects directory
	Window   int           // days in the series, including today
	MinEvery time.Duration // Report reuses a scan younger than this
	Now      func() time.Time

	mu      sync.Mutex
	offsets map[string]int64
	seen    map[string]string // message id → day, for de-duplication
	days    map[string]*bucket
	last    time.Time
	report  Report
}

// DefaultRoot is $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects.
func DefaultRoot() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

func New(root string) *Scanner {
	return &Scanner{Root: root, Window: 7, MinEvery: 15 * time.Second, Now: time.Now,
		offsets: map[string]int64{}, seen: map[string]string{}, days: map[string]*bucket{}}
}

// Report rescans if the last scan is older than MinEvery.
func (s *Scanner) Report() Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if s.last.IsZero() || now.Sub(s.last) >= s.MinEvery {
		s.scan(now)
		s.last = now
		s.report = s.build(now)
	}
	return s.report
}

func dayOf(t time.Time) string { return t.Local().Format("2006-01-02") }

func (s *Scanner) windowStart(now time.Time) time.Time {
	y, m, d := now.Local().Date()
	return time.Date(y, m, d-(s.Window-1), 0, 0, 0, 0, time.Local)
}

func (s *Scanner) scan(now time.Time) {
	start := s.windowStart(now)
	first := dayOf(start)
	for d := range s.days {
		if d < first {
			delete(s.days, d)
		}
	}
	for id, d := range s.seen {
		if d < first {
			delete(s.seen, id)
		}
	}
	filepath.WalkDir(s.Root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(start) {
			return nil
		}
		off := s.offsets[path]
		if info.Size() < off { // rewritten: start over (ids de-duplicate)
			off = 0
		}
		if info.Size() == off {
			return nil
		}
		s.offsets[path] = s.readFrom(path, off, first)
		return nil
	})
}

type record struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	RequestID   string `json:"requestId"`
	IsSidechain bool   `json:"isSidechain"`
	Cwd         string `json:"cwd"`
	Message     struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input       int64 `json:"input_tokens"`
			Output      int64 `json:"output_tokens"`
			CacheRead   int64 `json:"cache_read_input_tokens"`
			CacheCreate int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// readFrom consumes complete lines after off and returns the new offset
// (a trailing partial line is left for the next scan).
func (s *Scanner) readFrom(path string, off int64, firstDay string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return off
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return off
	}
	r := bufio.NewReaderSize(f, 64<<10)
	sub := strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator))
	var long bool // inside a line longer than maxLine
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			if !long {
				buf = append(buf, chunk...)
				if len(buf) > maxLine {
					long, buf = true, nil
				}
			}
			off += int64(len(chunk))
			continue
		}
		if err != nil { // EOF: partial line, re-read next time
			return off
		}
		off += int64(len(chunk))
		line := chunk
		if len(buf) > 0 {
			line = append(buf, chunk...)
		}
		if !long {
			s.line(line, sub, firstDay)
		}
		long, buf = false, nil
	}
}

func (s *Scanner) line(line []byte, sub bool, firstDay string) {
	if !bytes.Contains(line, []byte(`"usage"`)) || !bytes.Contains(line, []byte(`"assistant"`)) {
		return
	}
	var rec record
	if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" || rec.Message.Usage == nil {
		return
	}
	t, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
	if err != nil {
		return
	}
	day := dayOf(t)
	if day < firstDay {
		return
	}
	// one response is written once per content block, with the same usage
	if id := rec.Message.ID + "/" + rec.RequestID; rec.Message.ID != "" {
		if _, dup := s.seen[id]; dup {
			return
		}
		s.seen[id] = day
	}
	u := rec.Message.Usage
	c := Counts{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheCreate, Messages: 1}
	if c.Input+c.Output+c.CacheRead+c.CacheWrite == 0 {
		return // synthetic messages (errors, interruptions)
	}
	b := s.days[day]
	if b == nil {
		b = newBucket()
		s.days[day] = b
	}
	b.total.add(c)
	if sub || rec.IsSidechain {
		b.sub.add(c)
	}
	model := rec.Message.Model
	if model == "" {
		model = "unknown"
	}
	addTo(b.models, model, c)
	project := rec.Cwd
	if project == "" {
		project = "unknown"
	}
	addTo(b.projects, project, c)
}

func addTo(m map[string]*Counts, k string, c Counts) {
	if m[k] == nil {
		m[k] = &Counts{}
	}
	m[k].add(c)
}

func rows(m map[string]*Counts) []Named {
	out := []Named{}
	for k, c := range m {
		out = append(out, Named{Name: k, Counts: *c})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Input+out[i].Output+out[i].CacheWrite, out[j].Input+out[j].Output+out[j].CacheWrite
		if a != b {
			return a > b
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (s *Scanner) build(now time.Time) Report {
	r := Report{ScannedAt: now.Unix(), Models: []Named{}, Projects: []Named{}}
	start := s.windowStart(now)
	for i := 0; i < s.Window; i++ {
		d := dayOf(start.AddDate(0, 0, i))
		day := Day{Day: d}
		if b := s.days[d]; b != nil {
			day.Counts = b.total
		}
		r.Days = append(r.Days, day)
	}
	if b := s.days[dayOf(now)]; b != nil {
		r.Today, r.TodaySub = b.total, b.sub
		r.Models, r.Projects = rows(b.models), rows(b.projects)
	}
	return r
}
