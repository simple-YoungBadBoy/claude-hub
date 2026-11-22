package workspace

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRank(t *testing.T) {
	paths := []string{"web/", "web/src/", "web/src/App.tsx", "web/src/api.ts", "server/main.go", "docs/apple.md", "README.md"}
	if got := rank(paths, "app", 10); !reflect.DeepEqual(got, []string{"docs/apple.md", "web/src/App.tsx"}) {
		t.Fatalf("basename prefix, shorter first: %v", got)
	}
	if got := rank(paths, "ap", 10); got[len(got)-1] != "web/src/api.ts" && got[0] == "web/src/api.ts" {
		t.Fatalf("%v", got)
	}
	if got := rank(paths, "web/src/a", 10); !reflect.DeepEqual(got, []string{"web/src/api.ts", "web/src/App.tsx"}) {
		t.Fatalf("path prefix: %v", got)
	}
	if got := rank(paths, "web/src/", 10); !reflect.DeepEqual(got, []string{"web/src/api.ts", "web/src/App.tsx"}) {
		t.Fatalf("inside a directory, not the directory itself: %v", got)
	}
	if got := rank(paths, "smg", 10); !reflect.DeepEqual(got, []string{"server/main.go"}) {
		t.Fatalf("subsequence: %v", got)
	}
	if got := rank(paths, "", 3); !reflect.DeepEqual(got, []string{"web/", "README.md", "web/src/"}) {
		t.Fatalf("empty query: shallow first: %v", got)
	}
}

func TestCompleteFilesGitAndWalk(t *testing.T) {
	dir := gitInit(t)
	write(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	write(t, filepath.Join(dir, "ignored.txt"), "x")
	got := CompleteFiles(dir, "", 50)
	has := map[string]bool{}
	for _, p := range got {
		has[p] = true
	}
	if !has["a.txt"] || !has["new dir/"] || !has["new dir/新.md"] || has["ignored.txt"] {
		t.Fatalf("git index (tracked + untracked, ignored left out, dirs added): %v", got)
	}

	plain := t.TempDir()
	write(t, filepath.Join(plain, "src", "x.go"), "")
	write(t, filepath.Join(plain, "node_modules", "dep", "i.js"), "")
	write(t, filepath.Join(plain, ".hidden", "h"), "")
	if got := CompleteFiles(plain, "", 50); !reflect.DeepEqual(got, []string{"src/", "src/x.go"}) {
		t.Fatalf("walk outside git: %v", got)
	}
}

func TestCommands(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	proj := t.TempDir()
	if none := Commands(t.TempDir()); none == nil || len(none) != 0 {
		t.Fatalf("no commands is an empty list (JSON [], not null): %#v", none)
	}
	write(t, filepath.Join(proj, ".claude", "commands", "deploy.md"), "---\ndescription: \"部署到服务器\"\n---\nrun it")
	write(t, filepath.Join(proj, ".claude", "commands", "db", "migrate.md"), "no front matter")
	write(t, filepath.Join(cfg, "commands", "deploy.md"), "---\ndescription: user one\n---\n")
	write(t, filepath.Join(cfg, "skills", "pdf-tool", "SKILL.md"), "---\nname: pdf\ndescription: Work with PDFs\n---\n")
	write(t, filepath.Join(cfg, "skills", "broken", "README.md"), "")
	got := Commands(proj)
	want := []Command{
		{Name: "db:migrate", Source: "project"},
		{Name: "deploy", Desc: "部署到服务器", Source: "project"},
		{Name: "pdf", Desc: "Work with PDFs", Source: "user"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
}
