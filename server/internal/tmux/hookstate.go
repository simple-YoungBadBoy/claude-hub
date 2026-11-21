package tmux

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// HookEvent is what `claude-hub hook` forwards for one Claude Code hook call.
// Only these fields are sent: never prompts, tool inputs or outputs.
type HookEvent struct {
	Pane       string `json:"pane"`       // $TMUX_PANE, e.g. "%3"
	Event      string `json:"event"`      // hook_event_name
	Session    string `json:"session"`    // Claude session id
	Transcript string `json:"transcript"` // transcript_path
	Tool       string `json:"tool,omitempty"`
	NType      string `json:"ntype,omitempty"`   // notification_type
	Message    string `json:"message,omitempty"` // notification message, clipped
}

// Context is how full a session's context window is.
type Context struct {
	Tokens int     `json:"tokens"`
	Limit  int     `json:"limit"`
	Pct    float64 `json:"pct"` // 0..100
	Model  string  `json:"model,omitempty"`
}

// paneState is what hooks told us about one pane.
type paneState struct {
	state    string // working | tool | waiting | idle | error
	detail   string // tool name / permission message
	session  string
	transcr  string
	at       time.Time
	stale    int // polls where the screen disagrees with "working"
	ctx      *Context
	ctxKey   string // transcript size+mtime the context was read at
	sawHooks bool
}

// HookTracker keeps per-pane state from hook events.
type HookTracker struct {
	mu    sync.Mutex
	panes map[string]*paneState
	last  time.Time
}

func NewHookTracker() *HookTracker { return &HookTracker{panes: map[string]*paneState{}} }

// LastEvent is when any hook event arrived (zero if never).
func (h *HookTracker) LastEvent() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// Apply records a hook event.
func (h *HookTracker) Apply(e HookEvent) {
	if e.Pane == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last = time.Now()
	p := h.panes[e.Pane]
	if p == nil {
		p = &paneState{state: "idle"}
		h.panes[e.Pane] = p
	}
	p.sawHooks = true
	p.at = time.Now()
	if e.Session != "" && e.Session != p.session {
		p.session, p.ctx, p.ctxKey = e.Session, nil, ""
	}
	if e.Transcript != "" {
		p.transcr = e.Transcript
	}
	switch e.Event {
	case "SessionStart":
		p.state, p.detail = "idle", ""
	case "UserPromptSubmit", "PostToolUse", "PermissionDenied":
		p.state, p.detail = "working", ""
	case "PreCompact":
		p.state, p.detail = "working", "压缩上下文"
	case "PreToolUse":
		p.state, p.detail = "tool", e.Tool
	case "PermissionRequest":
		p.state, p.detail = "waiting", e.Tool
	case "Notification":
		// permission prompts and questions need the user; idle reminders don't
		if e.NType == "" || strings.Contains(e.NType, "permission") || strings.Contains(e.NType, "elicitation") {
			if e.NType != "idle_prompt" {
				p.state, p.detail = "waiting", e.Message
			}
		}
	case "PostToolUseFailure":
		p.state, p.detail = "working", "工具出错："+e.Tool
	case "StopFailure":
		p.state, p.detail = "error", e.Message
	case "Stop":
		p.state, p.detail = "idle", ""
	case "SessionEnd":
		delete(h.panes, e.Pane)
	}
}

// prune forgets panes that no longer exist (killed without SessionEnd).
func (h *HookTracker) prune(alive map[string]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id := range h.panes {
		if !alive[id] {
			delete(h.panes, id)
		}
	}
}

// resolve combines hook state with what the screen shows. ok=false means no
// hook information for this pane: fall back to the screen alone.
func (h *HookTracker) resolve(pane, screen string) (status, detail string, ctx *Context, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := h.panes[pane]
	if p == nil || !p.sawHooks {
		return "", "", nil, false
	}
	st := p.state
	switch {
	case (st == "working" || st == "tool") && screen != "working" && screen != "waiting":
		// Esc interrupts don't fire Stop: trust the screen after ~6s
		p.stale++
		if p.stale >= 3 && time.Since(p.at) > 5*time.Second {
			p.state, p.detail, p.stale = "idle", "", 0
			st = "idle"
		}
	case st == "waiting" && screen == "working" && time.Since(p.at) > 3*time.Second:
		p.state, p.detail = "working", ""
		st = "working"
	case (st == "idle" || st == "error") && screen == "working":
		st = "working" // a turn started before hooks were installed
	default:
		p.stale = 0
	}
	// read context only when the transcript changed
	if p.transcr != "" {
		if fi, err := os.Stat(p.transcr); err == nil {
			key := fi.ModTime().String() + "/" + itoa(fi.Size())
			if key != p.ctxKey {
				p.ctxKey = key
				if c := ReadContext(p.transcr); c != nil {
					p.ctx = c
				}
			}
		}
	}
	status = st
	if st == "tool" {
		status = "working"
	}
	return status, describe(st, p.detail), p.ctx, true
}

func describe(state, detail string) string {
	switch state {
	case "tool":
		if detail != "" {
			return "运行 " + detail
		}
		return "运行工具"
	case "working":
		if detail != "" {
			return detail
		}
		return "思考中"
	case "waiting":
		if detail != "" {
			return "等你确认：" + detail
		}
		return "等你确认"
	case "error":
		if detail != "" {
			return "出错：" + detail
		}
		return "出错了"
	}
	return ""
}

func itoa(n int64) string {
	b := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// ReadContext reads the last assistant message's usage from a Claude Code
// transcript (.jsonl) and estimates how full the context window is.
func ReadContext(path string) *Context {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const tail = 512 * 1024
	if fi, err := f.Stat(); err == nil && fi.Size() > tail {
		f.Seek(fi.Size()-tail, io.SeekStart)
	}
	data, _ := io.ReadAll(f)
	var best *Context
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"usage"`)) {
			continue
		}
		var rec struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			Message     struct {
				Model string `json:"model"`
				Usage struct {
					Input       int `json:"input_tokens"`
					CacheRead   int `json:"cache_read_input_tokens"`
					CacheCreate int `json:"cache_creation_input_tokens"`
					Output      int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" || rec.IsSidechain {
			continue
		}
		u := rec.Message.Usage
		tokens := u.Input + u.CacheRead + u.CacheCreate + u.Output
		if tokens == 0 {
			continue
		}
		best = &Context{Tokens: tokens, Model: rec.Message.Model}
	}
	if best == nil {
		return nil
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
	return best
}
