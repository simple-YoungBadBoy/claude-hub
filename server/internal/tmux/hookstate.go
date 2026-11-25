package tmux

import (
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
	sum      *Summary
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
		p.session, p.ctx, p.sum, p.ctxKey = e.Session, nil, nil, ""
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
	p := h.panes[pane]
	if p == nil || !p.sawHooks {
		h.mu.Unlock()
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
	status, detail = st, describe(st, p.detail)
	if st == "tool" {
		status = "working"
	}
	transcr, ctxKey := p.transcr, p.ctxKey
	h.mu.Unlock()

	// Read context only when the transcript changed, and without the lock:
	// parsing can take a while and hook events must not wait for it.
	if transcr != "" {
		if fi, err := os.Stat(transcr); err == nil {
			if key := fi.ModTime().String() + "/" + itoa(fi.Size()); key != ctxKey {
				c, sum := ReadTranscript(transcr)
				h.mu.Lock()
				// skip if the pane moved on (new session or transcript) meanwhile
				if h.panes[pane] == p && p.transcr == transcr {
					p.ctxKey = key
					if c != nil {
						p.ctx = c
					}
					p.sum = keepTitle(sum, p.sum)
				}
				h.mu.Unlock()
			}
		}
	}
	h.mu.Lock()
	ctx = p.ctx
	h.mu.Unlock()
	return status, detail, ctx, true
}

// summary is the latest transcript summary of a hooked pane.
func (h *HookTracker) summary(pane string) *Summary {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.panes[pane]; p != nil {
		return p.sum
	}
	return nil
}

// keepTitle keeps the previous title when the tail no longer contains one
// (the AI title is written early; long turns push it out of the tail).
func keepTitle(now, prev *Summary) *Summary {
	if now == nil {
		return prev
	}
	if now.Title == "" && prev != nil {
		now.Title = prev.Title
	}
	return now
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

// transcript is the transcript path hooks reported for a pane.
func (h *HookTracker) transcript(pane string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.panes[pane]; p != nil {
		return p.transcr
	}
	return ""
}
