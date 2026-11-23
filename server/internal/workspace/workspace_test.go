package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadStaysInsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "proj")
	write(t, filepath.Join(root, "README.md"), "# 你好\n")
	write(t, filepath.Join(root, "sub", "a.go"), "package a\n")
	write(t, filepath.Join(base, "secret"), "token")
	os.Symlink(filepath.Join(base, "secret"), filepath.Join(root, "link"))
	os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "sublink"))

	v, err := Read(root, "README.md")
	if err != nil || v.Text != "# 你好\n" || v.Dir {
		t.Fatalf("%+v %v", v, err)
	}
	if v, err := Read(root, filepath.Join(root, "sub", "a.go")); err != nil || v.Path != "sub/a.go" {
		t.Fatalf("absolute path inside root: %+v %v", v, err)
	}
	if v, err := Read(root, "sublink/a.go"); err != nil || v.Text != "package a\n" {
		t.Fatalf("symlink inside root: %+v %v", v, err)
	}
	for _, bad := range []string{"../secret", "link", filepath.Join(base, "secret"), "sub/../../secret", "/etc/passwd"} {
		if _, err := Read(root, bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	d, err := Read(root, "")
	if err != nil || !d.Dir || d.Path != "" {
		t.Fatalf("%+v %v", d, err)
	}
	if d.Entries[0].Name != "sub" || !d.Entries[0].Dir {
		t.Fatalf("dirs first: %+v", d.Entries)
	}
}

func TestReadBinaryAndLarge(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "bin"), "\x89PNG\x00\x01")
	if v, err := Read(root, "bin"); err != nil || !v.Binary || v.Text != "" {
		t.Fatalf("%+v %v", v, err)
	}
	big := strings.Repeat("中", MaxFile/3+10) // 3 bytes each: the cut lands mid-rune
	write(t, filepath.Join(root, "big.txt"), big)
	v, err := Read(root, "big.txt")
	if err != nil || !v.Truncated || v.Binary || len(v.Text) > MaxFile || !strings.HasSuffix(v.Text, "中") {
		t.Fatalf("truncated=%v binary=%v len=%d err=%v", v.Truncated, v.Binary, len(v.Text), err)
	}
}

func gitInit(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	write(t, filepath.Join(dir, "gone.txt"), "x\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	write(t, filepath.Join(dir, "a.txt"), "one\n2\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
	write(t, filepath.Join(dir, "new dir", "新.md"), "hello\nworld\n")
	return dir
}

func TestGitStatusAndDiff(t *testing.T) {
	dir := gitInit(t)
	g, err := Status(filepath.Join(dir, "new dir")) // from a subdirectory
	if err != nil || !g.Repo || g.Branch != "main" {
		t.Fatalf("%+v %v", g, err)
	}
	got := map[string]string{}
	for _, c := range g.Changes {
		got[c.Path] = c.Status
	}
	if got["a.txt"] != " M" || got["gone.txt"] != " D" || got["new dir/新.md"] != "??" || len(got) != 3 {
		t.Fatalf("changes %+v", g.Changes)
	}
	d, err := FileDiff(dir, "a.txt")
	if err != nil || !strings.Contains(d.Text, "-two\n+2\n") {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = FileDiff(dir, "new dir/新.md")
	if err != nil || !strings.Contains(d.Text, "+hello\n+world\n") {
		t.Fatalf("untracked: %+v %v", d, err)
	}
	if _, err := FileDiff(dir, "../etc/passwd"); err == nil {
		t.Fatal("a path git doesn't report must be refused")
	}
	if _, err := FileDiff(dir, "--output=/tmp/x"); err == nil {
		t.Fatal("option-looking path must be refused")
	}
}

func TestNotARepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	g, err := Status(t.TempDir())
	if err != nil || g.Repo || g.Changes == nil {
		t.Fatalf("%+v %v", g, err)
	}
}

func TestParseStatusRename(t *testing.T) {
	ch, trunc := parseStatus([]byte("R  new.go\x00old.go\x00?? x\x00"))
	if trunc || len(ch) != 2 || ch[0].Path != "new.go" || ch[0].From != "old.go" || ch[1].Path != "x" {
		t.Fatalf("%+v", ch)
	}
}

func TestCommits(t *testing.T) {
	dir := gitInit(t)
	l, err := Commits(dir)
	if err != nil || len(l.Commits) != 1 || l.Commits[0].Subject != "init" || l.Upstream != "" || l.Commits[0].Pushed {
		t.Fatalf("%+v %v", l, err)
	}
	// a clone has an upstream: its own new commit is unpushed, the cloned one pushed
	clone := filepath.Join(t.TempDir(), "c")
	run := func(d string, args ...string) {
		cmd := exec.Command("git", append([]string{"-C", d, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run(dir, "clone", "-q", dir, clone)
	write(t, filepath.Join(clone, "b.txt"), "b\n")
	run(clone, "add", ".")
	run(clone, "commit", "-q", "-m", "加 b")
	l, err = Commits(clone)
	if err != nil || l.Upstream != "origin/main" || l.Ahead != 1 || l.Behind != 0 || len(l.Commits) != 2 ||
		l.Commits[0].Pushed || l.Commits[0].Subject != "加 b" || !l.Commits[1].Pushed {
		t.Fatalf("%+v %v", l, err)
	}
	d, err := CommitDiff(clone, l.Commits[0].Short)
	if err != nil || !strings.Contains(d.Text, "+b\n") || !strings.Contains(d.Text, "    加 b") || !strings.Contains(d.Text, "b.txt | 1 +") {
		t.Fatalf("%+v %v", d, err)
	}
	for _, bad := range []string{"HEAD", "--output=/tmp/x", "abc", "0000000"} {
		if _, err := CommitDiff(clone, bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	if l, _ := Commits(t.TempDir()); l == nil || len(l.Commits) != 0 {
		t.Fatal("not a repo")
	}
}
