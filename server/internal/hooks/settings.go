package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Events we subscribe to. Tool events get matcher "*".
var Events = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure",
	"PermissionRequest", "Notification", "PreCompact", "Stop", "StopFailure", "SessionEnd",
}

var toolEvents = map[string]bool{"PreToolUse": true, "PostToolUse": true, "PostToolUseFailure": true, "PermissionRequest": true}

// marker identifies our entries: the command always ends with " hook" and
// contains this binary name.
const marker = "claude-hub"

func ours(cmd string) bool {
	return strings.Contains(cmd, marker) && strings.HasSuffix(strings.TrimSpace(cmd), " hook")
}

// SettingsPath is ~/.claude/settings.json (or $CLAUDE_CONFIG_DIR/settings.json).
func SettingsPath() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "settings.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "settings.json")
}

// Command is the hook command for this binary.
func Command() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if strings.ContainsAny(exe, " '\"") {
		return fmt.Sprintf("%q hook", exe), nil
	}
	return exe + " hook", nil
}

type entry struct {
	Matcher string `json:"matcher,omitempty"`
	Hooks   []hook `json:"hooks"`
}

type hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

func load(path string) (map[string]json.RawMessage, error) {
	settings := map[string]json.RawMessage{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return settings, nil
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		return nil, fmt.Errorf("%s 不是合法 JSON，未做修改: %w", path, err)
	}
	return settings, nil
}

// strip removes our entries from a hooks object, keeping everything else.
func strip(hooksRaw json.RawMessage) (map[string][]json.RawMessage, error) {
	out := map[string][]json.RawMessage{}
	if len(hooksRaw) == 0 {
		return out, nil
	}
	var byEvent map[string][]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &byEvent); err != nil {
		return nil, fmt.Errorf("settings.json 里的 hooks 格式不认识，未做修改: %w", err)
	}
	for ev, list := range byEvent {
		for _, raw := range list {
			var e entry
			if json.Unmarshal(raw, &e) == nil && len(e.Hooks) > 0 {
				keep := e.Hooks[:0]
				for _, h := range e.Hooks {
					if !ours(h.Command) {
						keep = append(keep, h)
					}
				}
				if len(keep) == 0 {
					continue
				}
				if len(keep) != len(e.Hooks) {
					e.Hooks = keep
					raw, _ = json.Marshal(e)
				}
			}
			out[ev] = append(out[ev], raw)
		}
	}
	return out, nil
}

func save(path string, settings map[string]json.RawMessage) (backup string, err error) {
	if old, err := os.ReadFile(path); err == nil {
		backup = fmt.Sprintf("%s.bak-claude-hub-%s", path, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return "", err
		}
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	tmp := path + ".tmp-claude-hub"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	return backup, os.Rename(tmp, path)
}

// Install adds our hook to every event (replacing older copies of ours).
func Install(path, command string) (backup string, err error) {
	settings, err := load(path)
	if err != nil {
		return "", err
	}
	byEvent, err := strip(settings["hooks"])
	if err != nil {
		return "", err
	}
	for _, ev := range Events {
		e := entry{Hooks: []hook{{Type: "command", Command: command, Timeout: 5}}}
		if toolEvents[ev] {
			e.Matcher = "*"
		}
		raw, _ := json.Marshal(e)
		byEvent[ev] = append(byEvent[ev], raw)
	}
	h, _ := json.Marshal(byEvent)
	settings["hooks"] = h
	return save(path, settings)
}

// Uninstall removes only our entries.
func Uninstall(path string) (backup string, err error) {
	settings, err := load(path)
	if err != nil {
		return "", err
	}
	if _, ok := settings["hooks"]; !ok {
		return "", nil
	}
	byEvent, err := strip(settings["hooks"])
	if err != nil {
		return "", err
	}
	if len(byEvent) == 0 {
		delete(settings, "hooks")
	} else {
		h, _ := json.Marshal(byEvent)
		settings["hooks"] = h
	}
	return save(path, settings)
}

// Installed reports which of our events are present.
func Installed(path string) (n int, total int) {
	settings, err := load(path)
	if err != nil {
		return 0, len(Events)
	}
	var byEvent map[string][]entry
	json.Unmarshal(settings["hooks"], &byEvent)
	for _, ev := range Events {
		for _, e := range byEvent[ev] {
			found := false
			for _, h := range e.Hooks {
				if ours(h.Command) {
					found = true
				}
			}
			if found {
				n++
				break
			}
		}
	}
	return n, len(Events)
}
