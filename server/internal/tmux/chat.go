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
	Name  string `json:"name"`
	Input string `json:"input,omitempty"` // the interesting argument, clipped
	Error bool   `json:"error,omitempty"` // its result was an error
}

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
				Type      string `json:"type"`
				ToolUseID string `json:"tool_use_id"`
				IsError   bool   `json:"is_error"`
			}
			if json.Unmarshal(rec.Message.Content, &blocks) == nil && len(blocks) > 0 && blocks[0].Type == "tool_result" {
				for _, b := range blocks {
					if at, ok := toolAt[b.ToolUseID]; ok && b.IsError && at[0] < len(msgs) {
						msgs[at[0]].Tools[at[1]].Error = true
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
						m.Tools = append(m.Tools, ChatTool{Name: b.Name, Input: toolInput(b.Input)})
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
	chat.Messages = append(chat.Messages, out...)
	return chat, nil
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
