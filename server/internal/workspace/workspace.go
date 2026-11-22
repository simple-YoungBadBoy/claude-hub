// Package workspace reads a session's working directory for the web UI:
// git changes, files and directories. Everything is read-only, confined to
// the directory and capped in size.
package workspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxFile    = 512 * 1024 // bytes of a file shown
	MaxDiff    = 256 * 1024 // bytes of a diff shown
	MaxEntries = 500        // files listed in a directory or git status
)

// ---- files ----

// Entry is one item of a directory listing.
type Entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// View is a file's text or a directory's entries.
type View struct {
	Path      string  `json:"path"` // relative to the root, "" for the root
	Dir       bool    `json:"dir"`
	Text      string  `json:"text,omitempty"`
	Binary    bool    `json:"binary,omitempty"`
	Size      int64   `json:"size"`
	Truncated bool    `json:"truncated,omitempty"`
	Entries   []Entry `json:"entries,omitempty"`
}

var ErrOutside = errors.New("只能查看会话工作目录里的文件")

// Resolve maps rel (relative to root, or absolute inside it) to a real path,
// following symlinks, and refuses anything that ends up outside root.
func Resolve(root, rel string) (real, clean string, err error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	p := rel
	if !filepath.IsAbs(p) {
		p = filepath.Join(rootReal, p)
	} else if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		p = filepath.Join(r, filepath.Base(p)) // the root itself may be a symlink
	}
	real, err = filepath.EvalSymlinks(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", errors.New("文件不存在")
		}
		return "", "", err
	}
	clean, err = filepath.Rel(rootReal, real)
	if err != nil || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", ErrOutside
	}
	if clean == "." {
		clean = ""
	}
	return real, filepath.ToSlash(clean), nil
}

// Read returns a file (text only, first MaxFile bytes) or a directory listing.
func Read(root, rel string) (*View, error) {
	real, clean, err := Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	v := &View{Path: clean, Size: fi.Size()}
	if fi.IsDir() {
		v.Dir = true
		v.Entries, v.Truncated, err = list(real)
		return v, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("不是普通文件")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFile {
		data, v.Truncated = data[:MaxFile], true
		for i := 0; i < 3 && len(data) > 0; i++ { // don't cut a rune in half
			if r, size := utf8.DecodeLastRune(data); r != utf8.RuneError || size != 1 {
				break
			}
			data = data[:len(data)-1]
		}
	}
	if isBinary(data) {
		v.Binary = true
		return v, nil
	}
	v.Text = string(data)
	return v, nil
}

func isBinary(b []byte) bool {
	head := b
	if len(head) > 8192 {
		head = head[:8192]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	// allow a rune cut at the end of the head
	for i := 0; i < 4 && len(head) > 0; i++ {
		if utf8.Valid(head) {
			return false
		}
		head = head[:len(head)-1]
	}
	return !utf8.Valid(head)
}

func list(dir string) ([]Entry, bool, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, err
	}
	out := make([]Entry, 0, len(ents))
	for _, e := range ents {
		if e.Name() == ".git" {
			continue
		}
		en := Entry{Name: e.Name(), Dir: e.IsDir()}
		if e.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, e.Name())); err == nil {
				en.Dir = fi.IsDir()
			}
		}
		if !en.Dir {
			if fi, err := e.Info(); err == nil {
				en.Size = fi.Size()
			}
		}
		out = append(out, en)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > MaxEntries {
		return out[:MaxEntries], true, nil
	}
	return out, false, nil
}

// ---- git ----

// Change is one changed file in `git status`.
type Change struct {
	Path   string `json:"path"`
	From   string `json:"from,omitempty"` // renamed from
	Status string `json:"status"`         // two-letter porcelain code, e.g. " M", "A ", "??"
}

// Git is the state of the repository containing a directory.
type Git struct {
	Repo      bool     `json:"repo"`
	Root      string   `json:"root,omitempty"`
	Branch    string   `json:"branch,omitempty"`
	Changes   []Change `json:"changes"`
	Truncated bool     `json:"truncated,omitempty"`
}

// git runs git read-only: no optional locks (Claude may be committing at the
// same time), and no repo-configured helpers that would run programs.
func git(dir string, args ...string) ([]byte, error) { return gitCapped(dir, MaxDiff+1, args...) }

func gitCapped(dir string, max int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	base := []string{"-C", dir,
		"-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false",
		"-c", "diff.external=", "-c", "core.pager=cat", "-c", "color.ui=false"}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout = &limited{w: &out, n: max}
	cmd.Stderr = &limited{w: &errb, n: 4096}
	err := cmd.Run()
	if err != nil && out.Len() == 0 {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out.Bytes(), nil
}

// limited keeps the first n bytes and discards the rest (git keeps writing).
type limited struct {
	w io.Writer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// Status lists the changes of the repository containing dir.
func Status(dir string) (*Git, error) {
	root, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return &Git{Changes: []Change{}}, nil // not a repository
	}
	g := &Git{Repo: true, Root: strings.TrimSpace(string(root)), Changes: []Change{}}
	if b, err := git(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		g.Branch = strings.TrimSpace(string(b))
	}
	out, err := git(g.Root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	g.Changes, g.Truncated = parseStatus(out)
	return g, nil
}

func parseStatus(out []byte) ([]Change, bool) {
	parts := strings.Split(string(out), "\x00")
	changes := []Change{}
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if len(p) < 4 {
			continue
		}
		c := Change{Status: p[:2], Path: p[3:]}
		if c.Status[0] == 'R' || c.Status[0] == 'C' {
			if i+1 < len(parts) {
				c.From = parts[i+1]
				i++
			}
		}
		if len(changes) == MaxEntries {
			return changes, true
		}
		changes = append(changes, c)
	}
	return changes, false
}

// Diff is the patch of one changed file against HEAD (staged and unstaged
// together); an untracked file shows as all added.
type Diff struct {
	Path      string `json:"path"`
	Text      string `json:"text"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

func FileDiff(dir, path string) (*Diff, error) {
	g, err := Status(dir)
	if err != nil {
		return nil, err
	}
	if !g.Repo {
		return nil, errors.New("不是 git 仓库")
	}
	var ch *Change
	for i := range g.Changes {
		if g.Changes[i].Path == path {
			ch = &g.Changes[i]
		}
	}
	if ch == nil { // only files git reports as changed: path never reaches git unchecked
		return nil, errors.New("这个文件没有改动")
	}
	d := &Diff{Path: path}
	var out []byte
	switch {
	case ch.Status == "??":
		v, err := Read(g.Root, path)
		if err != nil {
			return nil, err
		}
		if v.Binary {
			d.Binary = true
			return d, nil
		}
		d.Text, d.Truncated = addedPatch(path, v.Text), v.Truncated
		return d, nil
	default:
		args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "-M"}
		if _, err := git(g.Root, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
			args = append(args, "HEAD")
		} else {
			args = append(args, "--cached") // no commits yet
		}
		args = append(args, "--", path)
		if ch.From != "" {
			args = append(args, ch.From)
		}
		out, err = git(g.Root, args...)
		if err != nil {
			return nil, err
		}
	}
	if len(out) > MaxDiff {
		out, d.Truncated = out[:MaxDiff], true
	}
	if bytes.Contains(out, []byte("\nBinary files ")) || bytes.HasPrefix(out, []byte("Binary files ")) {
		d.Binary = true
	}
	d.Text = strings.ToValidUTF8(string(out), "�")
	return d, nil
}

func addedPatch(path, text string) string {
	var b strings.Builder
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	b.WriteString("--- /dev/null\n+++ b/" + path + "\n")
	b.WriteString("@@ -0,0 +1," + itoa(len(lines)) + " @@\n")
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
