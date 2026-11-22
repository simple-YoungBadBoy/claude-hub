package workspace

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---- @file completion ----

const maxIndexed = 50000

type fileIndex struct {
	at    time.Time
	paths []string // files and directories ("dir/"), relative to the root
}

var (
	idxMu sync.Mutex
	idx   = map[string]*fileIndex{}
)

// index lists the files under dir (respecting .gitignore in a repository),
// cached for a few seconds: completion asks on every keystroke.
func index(dir string) []string {
	idxMu.Lock()
	defer idxMu.Unlock()
	if c := idx[dir]; c != nil && time.Since(c.at) < 10*time.Second {
		return c.paths
	}
	for k, c := range idx { // forget other directories after a while
		if time.Since(c.at) > 5*time.Minute {
			delete(idx, k)
		}
	}
	var files []string
	if out, err := gitAll(dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard"); err == nil {
		for _, p := range strings.Split(string(out), "\x00") {
			if p != "" && len(files) < maxIndexed {
				files = append(files, p)
			}
		}
	} else {
		files = walk(dir)
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(files)+len(files)/4)
	for _, f := range files {
		for d := filepath.Dir(f); d != "." && d != "/" && !seen[d]; d = filepath.Dir(d) {
			seen[d] = true
			paths = append(paths, d+"/")
		}
		paths = append(paths, f)
	}
	idx[dir] = &fileIndex{at: time.Now(), paths: paths}
	return paths
}

// gitAll is git() without the diff-sized output cap.
func gitAll(dir string, args ...string) ([]byte, error) {
	if _, err := git(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return nil, err
	}
	return gitCapped(dir, 32<<20, args...)
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true, ".next": true, "dist": true, "build": true, "target": true, ".cache": true}

// walk lists files outside a repository: hidden and dependency directories
// skipped, capped in count and depth.
func walk(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= maxIndexed {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator)) >= 6 {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out
}

// CompleteFiles ranks paths under dir for what was typed after "@".
func CompleteFiles(dir, q string, n int) []string {
	return rank(index(dir), q, n)
}

func rank(paths []string, q string, n int) []string {
	q = strings.ToLower(strings.TrimPrefix(q, "./"))
	type hit struct {
		p     string
		score int
	}
	var hits []hit
	for _, p := range paths {
		lp := strings.ToLower(p)
		base := strings.ToLower(filepath.Base(strings.TrimSuffix(p, "/")))
		s := -1
		switch {
		case lp == q: // already typed in full
		case q == "":
			s = strings.Count(strings.TrimSuffix(p, "/"), "/") // shallow first
		case strings.HasPrefix(lp, q):
			s = 0
		case strings.HasPrefix(base, q):
			s = 1
		case strings.Contains(base, q):
			s = 2
		case strings.Contains(lp, q):
			s = 3
		case subseq(lp, q):
			s = 4
		}
		if s >= 0 {
			hits = append(hits, hit{p, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		if len(hits[i].p) != len(hits[j].p) {
			return len(hits[i].p) < len(hits[j].p)
		}
		return hits[i].p < hits[j].p
	})
	out := []string{}
	for i := 0; i < len(hits) && i < n; i++ {
		out = append(out, hits[i].p)
	}
	return out
}

func subseq(s, q string) bool {
	for _, r := range q {
		i := strings.IndexRune(s, r)
		if i < 0 {
			return false
		}
		s = s[i+len(string(r)):]
	}
	return true
}

// ---- /command completion ----

// Command is a custom slash command or skill Claude Code would offer.
type Command struct {
	Name   string `json:"name"` // without the slash
	Desc   string `json:"desc,omitempty"`
	Source string `json:"source"` // project | user
}

// ConfigDir is $CLAUDE_CONFIG_DIR or ~/.claude.
func ConfigDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// Commands lists custom commands (.claude/commands/**/*.md, nested
// directories become "dir:name") and skills (.claude/skills/*/SKILL.md) of
// the project at cwd and of the user. Project entries win on a name clash.
func Commands(cwd string) []Command {
	out := []Command{}
	seen := map[string]bool{}
	for _, src := range []struct{ dir, name string }{{filepath.Join(cwd, ".claude"), "project"}, {ConfigDir(), "user"}} {
		cmds := filepath.Join(src.dir, "commands")
		filepath.WalkDir(cmds, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".md") || len(out) >= 500 {
				return nil
			}
			rel, _ := filepath.Rel(cmds, p)
			name := strings.ReplaceAll(filepath.ToSlash(strings.TrimSuffix(rel, ".md")), "/", ":")
			if !seen[name] {
				seen[name] = true
				out = append(out, Command{Name: name, Desc: frontDesc(p, ""), Source: src.name})
			}
			return nil
		})
		ents, _ := os.ReadDir(filepath.Join(src.dir, "skills"))
		for _, e := range ents {
			p := filepath.Join(src.dir, "skills", e.Name(), "SKILL.md")
			if _, err := os.Stat(p); err != nil || len(out) >= 500 {
				continue
			}
			name := frontField(p, "name")
			if name == "" {
				name = e.Name()
			}
			if !seen[name] {
				seen[name] = true
				out = append(out, Command{Name: name, Desc: frontDesc(p, ""), Source: src.name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func frontDesc(path, def string) string {
	if d := frontField(path, "description"); d != "" {
		return d
	}
	return def
}

// frontField reads one field of a Markdown file's YAML front matter.
func frontField(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 8192)
	for n := 0; sc.Scan() && n < 40; n++ {
		l := sc.Text()
		if n == 0 {
			if strings.TrimSpace(l) != "---" {
				return ""
			}
			continue
		}
		if strings.TrimSpace(l) == "---" {
			return ""
		}
		if v, ok := strings.CutPrefix(l, key+":"); ok {
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if r := []rune(v); len(r) > 120 {
				v = string(r[:119]) + "…"
			}
			return v
		}
	}
	return ""
}
