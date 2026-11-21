package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const userSettings = `{
  "model": "opus",
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/local/bin/guard.sh"}]}]
  },
  "statusLine": {"type": "command", "command": "~/status.sh"}
}`

func TestInstallKeepsUserSettingsAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(userSettings), 0o600)
	cmd := "/opt/claude-hub/bin/claude-hub hook"
	for i := 0; i < 2; i++ { // twice: must not duplicate
		backup, err := Install(path, cmd)
		if err != nil {
			t.Fatal(err)
		}
		if backup == "" {
			t.Fatal("no backup")
		}
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	for _, want := range []string{`"model": "opus"`, "Bash(ls:*)", "say done", "guard.sh", "~/status.sh"} {
		if !strings.Contains(s, want) {
			t.Fatalf("lost user setting %q:\n%s", want, s)
		}
	}
	if n := strings.Count(s, cmd); n != len(Events) {
		t.Fatalf("our hook appears %d times, want %d", n, len(Events))
	}
	if n, total := Installed(path); n != total {
		t.Fatalf("installed %d/%d", n, total)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}

	if _, err := Uninstall(path); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	s = string(b)
	if strings.Contains(s, cmd) {
		t.Fatal("uninstall left our hook")
	}
	for _, want := range []string{"say done", "guard.sh", "~/status.sh"} {
		if !strings.Contains(s, want) {
			t.Fatalf("uninstall lost %q", want)
		}
	}
}

func TestInstallRefusesBrokenJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"model": "opus",`), 0o600)
	if _, err := Install(path, "x/claude-hub hook"); err == nil {
		t.Fatal("installed into broken JSON")
	}
	b, _ := os.ReadFile(path)
	if string(b) != `{"model": "opus",` {
		t.Fatal("broken file was modified")
	}
}

func TestInstallCreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	if _, err := Install(path, "/x/claude-hub hook"); err != nil {
		t.Fatal(err)
	}
	if n, total := Installed(path); n != total {
		t.Fatalf("installed %d/%d", n, total)
	}
}

func TestEventNeverForwardsPayloads(t *testing.T) {
	e := Event("%1", Input{Event: "PreToolUse", Tool: "Bash", Message: strings.Repeat("长", 500)})
	b, _ := json.Marshal(e)
	if len([]rune(e.Message)) > 121 {
		t.Fatal("message not clipped")
	}
	if strings.Contains(string(b), "tool_input") || strings.Contains(string(b), "prompt") {
		t.Fatal("payload leaked")
	}
}
