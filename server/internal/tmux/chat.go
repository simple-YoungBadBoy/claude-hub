package tmux

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// ChatTool is one tool call, shown collapsed.
type ChatTool struct {
	Name    string `json:"name"`
	Input   string `json:"input,omitempty"`   // the interesting argument, clipped
	Error   bool   `json:"error,omitempty"`   // its result was an error
	Output  string `json:"output,omitempty"`  // the start of its result
	Running bool   `json:"running,omitempty"` // no result yet
}

const (
	outputRunes  = 1200    // of one tool result
	outputLines  = 24      // of one tool result
	outputBudget = 400_000 // bytes of results in one chat, newest kept
)

// ChatMsg is one turn of a conversation as a reader sees it.
type ChatMsg struct {
	Role  string     `json:"role"` // user | assistant
	Text  string     `json:"text,omitempty"`
	Tools []ChatTool `json:"tools,omitempty"`
	At    string     `json:"at,omitempty"` // RFC 3339 timestamp of the first record
}

// Chat is the readable tail of a conversation.
type Chat struct {
	Messages  []ChatMsg `json:"messages"`
	Truncated bool      `json:"truncated,omitempty"` // older messages not shown
	Key       string    `json:"key,omitempty"`       // transcript version, sent back as ?since=
}

const chatTail = 2 << 20 // bytes of transcript read

// ReadChat turns the tail of a transcript into user / assistant messages:
// consecutive assistant records merge into one, tool calls are listed by
// name, tool results and Claude Code's own wrapper messages are left out.
func ReadChat(path string, max int) (*Chat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	chat := &Chat{Messages: []ChatMsg{}}
	if fi, err := f.Stat(); err == nil && fi.Size() > chatTail {
		f.Seek(fi.Size()-chatTail, io.SeekStart)
		chat.Truncated = true
	}
	data, _ := io.ReadAll(io.LimitReader(f, chatTail))
	if chat.Truncated { // drop the partial first line
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	var msgs []ChatMsg
	toolAt := map[string][2]int{} // tool_use id -> message, tool index
	for len(data) > 0 {
		line := data
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data = data[:i], data[i+1:]
		} else {
			data = nil
		}
		if len(line) == 0 || line[0] != '{' ||
			!(bytes.Contains(line, []byte(`"type":"assistant"`)) || bytes.Contains(line, []byte(`"type":"user"`))) {
			continue
		}
		var rec struct {
			Type        string `json:"type"`
			IsSidechain bool   `json:"isSidechain"`
			IsMeta      bool   `json:"isMeta"`
			Timestamp   string `json:"timestamp"`
			Message     struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.IsSidechain || rec.IsMeta {
			continue
		}
		switch rec.Type {
		case "user":
			// tool results: mark failed calls, otherwise skip
			var blocks []struct {
				Type      string          `json:"type"`
				ToolUseID string          `json:"tool_use_id"`
				IsError   bool            `json:"is_error"`
				Content   json.RawMessage `json:"content"`
			}
			if json.Unmarshal(rec.Message.Content, &blocks) == nil && len(blocks) > 0 && blocks[0].Type == "tool_result" {
				for _, b := range blocks {
					if at, ok := toolAt[b.ToolUseID]; ok && at[0] < len(msgs) {
						t := &msgs[at[0]].Tools[at[1]]
						t.Error, t.Running, t.Output = b.IsError, false, resultText(b.Content)
					}
				}
				continue
			}
			if t := promptText(rec.Message.Content); t != "" {
				msgs = append(msgs, ChatMsg{Role: "user", Text: clipRunes(t, 20000), At: rec.Timestamp})
			}
		case "assistant":
			var blocks []struct {
				Type  string          `json:"type"`
				Text  string          `json:"text"`
				ID    string          `json:"id"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if json.Unmarshal(rec.Message.Content, &blocks) != nil {
				continue
			}
			if len(msgs) == 0 || msgs[len(msgs)-1].Role != "assistant" {
				msgs = append(msgs, ChatMsg{Role: "assistant", At: rec.Timestamp})
			}
			m := &msgs[len(msgs)-1]
			for _, b := range blocks {
				switch b.Type {
				case "text":
					if t := strings.TrimSpace(b.Text); t != "" {
						if m.Text != "" {
							m.Text += "\n\n"
						}
						m.Text = clipRunes(m.Text+t, 40000)
					}
				case "tool_use":
					if len(m.Tools) < 200 {
						toolAt[b.ID] = [2]int{len(msgs) - 1, len(m.Tools)}
						m.Tools = append(m.Tools, ChatTool{Name: b.Name, Input: toolInput(b.Input), Running: true})
					}
				}
			}
		}
	}
	// an assistant record with only thinking leaves an empty message
	out := msgs[:0]
	for _, m := range msgs {
		if m.Text != "" || len(m.Tools) > 0 {
			out = append(out, m)
		}
	}
	if len(out) > max {
		out, chat.Truncated = out[len(out)-max:], true
	}
	budget := outputBudget // the newest results matter most: drop older ones past the budget
	for i := len(out) - 1; i >= 0; i-- {
		for j := len(out[i].Tools) - 1; j >= 0; j-- {
			t := &out[i].Tools[j]
			if budget -= len(t.Output); budget < 0 {
				t.Output = ""
			}
		}
	}
	chat.Messages = append(chat.Messages, out...)
	return chat, nil
}

// resultText is the start of a tool result: a string, or its text blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var blocks []struct{ Type, Text string }
		json.Unmarshal(raw, &blocks)
		var parts []string
		for _, b := range blocks {
			switch b.Type {
			case "text":
				parts = append(parts, b.Text)
			case "image":
				parts = append(parts, "[图片]")
			}
		}
		s = strings.Join(parts, "\n")
	}
	s = strings.TrimSpace(s)
	lines := strings.SplitN(s, "\n", outputLines+1)
	cut := len(lines) > outputLines
	if cut {
		s = strings.Join(lines[:outputLines], "\n")
	}
	if r := []rune(s); len(r) > outputRunes {
		s, cut = string(r[:outputRunes]), true
	}
	if cut {
		s += "\n…"
	}
	return s
}

// toolInput picks the argument that says what a tool call did.
func toolInput(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "path", "pattern", "url", "query", "description", "prompt", "skill"} {
		if v, ok := in[k].(string); ok && v != "" {
			return clip(v, 160)
		}
	}
	return ""
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "\n…（太长，已截断）"
}
