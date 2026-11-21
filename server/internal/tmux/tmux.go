// Package tmux wraps the tmux CLI: list / create / kill sessions, capture panes
// and paste text. Every call passes arguments directly (no shell).
package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Session is one tmux session as shown in the client.
type Session struct {
	Name     string `json:"name"`
	Created  int64  `json:"created"`  // unix seconds
	Attached int    `json:"attached"` // number of attached clients
	Command  string `json:"command"`  // current command of the active pane
	Path     string `json:"path"`     // cwd of the active pane
	Claude   bool   `json:"claude"`   // active pane runs claude
	Status   string `json:"status"`   // idle | working | waiting | "" (non-claude)
}

// Socket, when set, makes every call use a private tmux server
// (`tmux -L <Socket>`); tests use it so they never touch real sessions.
var Socket string

// Command builds a tmux command honoring Socket.
func Command(args ...string) *exec.Cmd {
	if Socket != "" {
		args = append([]string{"-L", Socket}, args...)
	}
	cmd := exec.Command("tmux", args...)
	cmd.Env = Env()
	return cmd
}

func run(args ...string) (string, error) {
	cmd := Command(args...)
	cmd.Env = Env()
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// Env is the process env without TMUX, so we can talk to / attach to the
// server even when the daemon itself was started from inside tmux.
func Env() []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// List returns all sessions, newest first.
func List() ([]Session, error) {
	out, err := run("list-sessions", "-F", "#{session_name}\t#{session_created}\t#{session_attached}")
	if err != nil {
		if strings.Contains(err.Error(), "no server running") || strings.Contains(err.Error(), "No such file") {
			return []Session{}, nil
		}
		return nil, err
	}
	byName := map[string]*Session{}
	var list []Session
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		c, _ := strconv.ParseInt(f[1], 10, 64)
		a, _ := strconv.Atoi(f[2])
		list = append(list, Session{Name: f[0], Created: c, Attached: a})
	}
	for i := range list {
		byName[list[i].Name] = &list[i]
	}
	panes, err := run("list-panes", "-a", "-F", "#{session_name}\t#{window_active}\t#{pane_active}\t#{pane_current_command}\t#{pane_current_path}")
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(panes), "\n") {
			f := strings.Split(line, "\t")
			if len(f) < 5 || f[1] != "1" || f[2] != "1" {
				continue
			}
			if s := byName[f[0]]; s != nil {
				s.Command, s.Path = f[3], f[4]
				s.Claude = isClaude(f[3])
			}
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Created > list[j].Created })
	return list, nil
}

func isClaude(cmd string) bool {
	c := strings.ToLower(cmd)
	return c == "claude" || strings.HasPrefix(c, "claude")
}

// SanitizeName turns user input into a valid tmux session name.
// tmux forbids '.' and ':'; we additionally keep it to letters, digits, '-', '_'.
func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '.' || r == ':' || r == '/':
			b.WriteRune('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// Exists reports whether a session with exactly this name exists.
func Exists(name string) bool {
	_, err := run("has-session", "-t", "="+name)
	return err == nil
}

// Create starts a detached session running command in dir and returns its name.
func Create(name, dir, command string) (string, error) {
	base := SanitizeName(name)
	if base == "" {
		base = time.Now().Format("0102-1504")
	}
	base = "cc-" + strings.TrimPrefix(base, "cc-")
	final := base
	for i := 2; Exists(final); i++ {
		final = fmt.Sprintf("%s-%d", base, i)
	}
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("目录不存在: %s", dir)
	}
	if _, err := run("new-session", "-d", "-s", final, "-c", dir, "-x", "160", "-y", "45", command); err != nil {
		return "", err
	}
	return final, nil
}

// Kill terminates a session.
func Kill(name string) error {
	_, err := run("kill-session", "-t", "="+name)
	return err
}

// Rename renames a session.
func Rename(old, name string) (string, error) {
	n := SanitizeName(name)
	if n == "" {
		return "", errors.New("名字无效")
	}
	if Exists(n) {
		return "", errors.New("已有同名会话")
	}
	_, err := run("rename-session", "-t", "="+old, n)
	return n, err
}

// Capture returns the visible text of the session's active pane.
func Capture(name string) (string, error) {
	return run("capture-pane", "-p", "-t", "="+name+":")
}

// Paste inserts text into the active pane as a bracketed paste (same as a
// terminal paste / drag-and-drop), so Claude Code treats image paths as
// attachments.
func Paste(name, text string) error {
	buf := fmt.Sprintf("claudehub-%d", time.Now().UnixNano())
	cmd := Command("load-buffer", "-b", buf, "-")
	cmd.Stdin = strings.NewReader(text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("load-buffer: %s", strings.TrimSpace(string(out)))
	}
	_, err := run("paste-buffer", "-p", "-d", "-b", buf, "-t", "="+name+":")
	return err
}
