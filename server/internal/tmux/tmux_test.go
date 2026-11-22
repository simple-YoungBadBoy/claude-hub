package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func privateServer(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	old := Socket
	Socket = fmt.Sprintf("claudehub-unit-%d", os.Getpid())
	t.Cleanup(func() {
		if path, err := run("display-message", "-p", "#{socket_path}"); err == nil {
			defer os.Remove(strings.TrimSpace(path))
		}
		Command("kill-server").Run()
		Socket = old
	})
}

func TestHubSessionsCountAsClaudeBeforeItStarts(t *testing.T) {
	privateServer(t)
	dir := t.TempDir()
	name, err := Create("unit", dir, "sleep 30") // not claude yet
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run("new-session", "-d", "-s", "plain", "-c", dir, "sleep 30"); err != nil {
		t.Fatal(err)
	}
	list, err := List()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Session{}
	for _, s := range list {
		got[s.Name] = s
	}
	if s := got[name]; !s.Hub || !s.Claude {
		t.Fatalf("hub session %+v should count as claude", s)
	}
	if s, ok := got["plain"]; !ok || s.Hub || s.Claude {
		t.Fatalf("server-side tmux session must be listed as non-claude: %+v (ok=%v)", s, ok)
	}
}

func TestNewServerStartsOutsideOurCgroup(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	if !scopeAvailable() {
		t.Skip("no systemd user manager")
	}
	// a fresh default socket in a temp dir: no server is running there
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX", "")
	old := Socket
	Socket = ""
	defer func() {
		Command("kill-server").Run()
		Socket = old
	}()
	if _, err := Create("scope", t.TempDir(), "sleep 30"); err != nil {
		t.Fatal(err)
	}
	pid, err := run("display-message", "-p", "#{pid}")
	if err != nil {
		t.Fatal(err)
	}
	cg, err := os.ReadFile("/proc/" + strings.TrimSpace(pid) + "/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cg), "tmux-claudehub-") {
		t.Fatalf("tmux server cgroup %q, want its own tmux-claudehub-* scope", cg)
	}
}

func TestLanded(t *testing.T) {
	scr := "old stuff\n────\n❯ 帮我把登录页的按钮改成\n  蓝色的\n────\n  ? for shortcuts\n"
	if !landed(scr, "帮我把登录页的按钮改成蓝色的") {
		t.Fatal("wrapped CJK input not found")
	}
	if landed("❯ \n", "hello world") {
		t.Fatal("empty input must not count")
	}
	if !landed("❯ [Pasted text #1 +12 lines]\n", "a\nb\nc") {
		t.Fatal("folded paste counts")
	}
}

func TestSendAndFastEscape(t *testing.T) {
	privateServer(t)
	if _, err := run("new-session", "-d", "-s", "s", "-x", "80", "-y", "20", "cat"); err != nil {
		t.Fatal(err)
	}
	run("set-option", "-gs", "escape-time", "500") // tmux's old default, whatever ~/.tmux.conf says
	FastEscape(10)
	if out, _ := run("show-options", "-gsv", "escape-time"); strings.TrimSpace(out) != "10" {
		t.Fatalf("escape-time %q", out)
	}
	run("set-option", "-gs", "escape-time", "300")
	FastEscape(10)
	if out, _ := run("show-options", "-gsv", "escape-time"); strings.TrimSpace(out) != "300" {
		t.Fatalf("a user's own value was changed: %q", out)
	}
	if err := Send("s", "/compact 你好"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	scr, _ := Capture("s")
	if strings.Count(scr, "/compact 你好") != 2 { // the tty echo and cat's copy after Enter
		t.Fatalf("screen:\n%s", scr)
	}
}
