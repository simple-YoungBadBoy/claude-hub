package tmux

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Summary is what a conversation is about, for the session list.
type Summary struct {
	Title string `json:"title,omitempty"` // Claude Code's AI title, else the last prompt
	Last  string `json:"last,omitempty"`  // Claude's last words since that prompt
}

// ReadContext reads how full a session's context window is.
func ReadContext(path string) *Context {
	c, _ := ReadTranscript(path)
	return c
}

// ReadTranscript reads the tail of a Claude Code transcript (.jsonl): the
// last main-thread usage (context fill) and a short summary.
func ReadTranscript(path string) (*Context, *Summary) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	const tail = 512 * 1024
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		f.Seek(fi.Size()-tail, io.SeekStart)
	}
	data, _ := io.ReadAll(io.LimitReader(f, tail))
	var best *Context
	var aiTitle, lastPrompt, userText, last string
	for len(data) > 0 { // no Scanner: one huge line must not hide the rest
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var rec struct {
			Type        string          `json:"type"`
			IsSidechain bool            `json:"isSidechain"`
			IsMeta      bool            `json:"isMeta"`
			AITitle     string          `json:"aiTitle"`
			LastPrompt  string          `json:"lastPrompt"`
			Message     json.RawMessage `json:"message"`
		}
		switch {
		case bytes.Contains(line, []byte(`"type":"assistant"`)),
			bytes.Contains(line, []byte(`"type":"user"`)),
			bytes.Contains(line, []byte(`"type":"ai-title"`)),
			bytes.Contains(line, []byte(`"type":"last-prompt"`)):
		default:
			continue
		}
		if json.Unmarshal(line, &rec) != nil || rec.IsSidechain {
			continue
		}
		var msg struct {
			Model   string          `json:"model"`
			Content json.RawMessage `json:"content"`
			Usage   *struct {
				Input       int `json:"input_tokens"`
				CacheRead   int `json:"cache_read_input_tokens"`
				CacheCreate int `json:"cache_creation_input_tokens"`
				Output      int `json:"output_tokens"`
			} `json:"usage"`
		}
		if len(rec.Message) > 0 {
			json.Unmarshal(rec.Message, &msg)
		}
		switch rec.Type {
		case "ai-title":
			aiTitle = rec.AITitle
		case "last-prompt":
			lastPrompt = rec.LastPrompt
		case "user":
			if t := promptText(msg.Content); t != "" && !rec.IsMeta {
				userText, last = t, "" // a new turn: the old reply is stale
			}
		case "assistant":
			if t := replyText(msg.Content); t != "" {
				last = t
			}
			if u := msg.Usage; u != nil {
				if tokens := u.Input + u.CacheRead + u.CacheCreate + u.Output; tokens > 0 {
					best = &Context{Tokens: tokens, Model: msg.Model}
				}
			}
		}
	}
	var sum *Summary
	title := aiTitle
	if title == "" {
		title = lastPrompt
	}
	if title == "" {
		title = userText
	}
	if title != "" || last != "" {
		sum = &Summary{Title: clip(title, 80), Last: clip(last, 160)}
	}
	if best == nil {
		return nil, sum
	}
	best.Limit = 200_000
	// Claude Code doesn't record the window size; above 200k it must be 1M
	if best.Tokens > best.Limit || strings.Contains(best.Model, "[1m]") {
		best.Limit = 1_000_000
	}
	best.Pct = float64(best.Tokens) * 100 / float64(best.Limit)
	if best.Pct > 100 {
		best.Pct = 100
	}
	return best, sum
}

// promptText is what the user typed: a plain string, or text blocks. Tool
// results and Claude Code's own wrapped messages (<command-name>…) are skipped.
func promptText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var blocks []struct{ Type, Text string }
		if json.Unmarshal(raw, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "tool_result" {
				return ""
			}
			if b.Type == "text" && s == "" {
				s = b.Text
			}
		}
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "<") || strings.HasPrefix(s, "[Request interrupted") {
		return ""
	}
	return s
}

func replyText(raw json.RawMessage) string {
	var blocks []struct{ Type, Text string }
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	t := ""
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			t = b.Text
		}
	}
	return t
}

var (
	mdNoise  = strings.NewReplacer("`", "", "**", "", "__", "", "#", "", "|", " ")
	htmlTag  = regexp.MustCompile(`</?[a-zA-Z][^>\n]*>`)
	tableSep = regexp.MustCompile(`(?m)^[\s|:-]*-{3,}[\s|:-]*$`)
)

// clip collapses whitespace, HTML tags, table rules and markdown noise and
// keeps at most n runes.
func clip(s string, n int) string {
	s = tableSep.ReplaceAllString(htmlTag.ReplaceAllString(s, " "), " ")
	s = strings.Join(strings.Fields(mdNoise.Replace(s)), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9-]`)

// ProjectsDir is $CLAUDE_CONFIG_DIR/projects or ~/.claude/projects.
func ProjectsDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// LatestTranscript guesses a session's transcript without hooks: the newest
// .jsonl in the project directory Claude Code derives from the cwd.
func LatestTranscript(projects, cwd string) string {
	dir := ProjectDir(projects, cwd)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestT int64
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().UnixNano() > bestT {
			best, bestT = filepath.Join(dir, e.Name()), fi.ModTime().UnixNano()
		}
	}
	return best
}
