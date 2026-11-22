package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
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
