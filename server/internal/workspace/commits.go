package workspace

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Commit is one commit on the current branch.
type Commit struct {
	Sha     string `json:"sha"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	At      int64  `json:"at"`
	Pushed  bool   `json:"pushed"` // reachable from the upstream (false without one)
}

// Log is the recent history of the branch, against its upstream if any.
type Log struct {
	Upstream string   `json:"upstream,omitempty"` // e.g. origin/main
	Ahead    int      `json:"ahead"`              // commits not pushed yet
	Behind   int      `json:"behind"`             // upstream commits not pulled
	Commits  []Commit `json:"commits"`
}

const maxCommits = 30

// Commits lists the newest commits of HEAD, marking which are not pushed.
func Commits(dir string) (*Log, error) {
	l := &Log{Commits: []Commit{}}
	if _, err := git(dir, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return l, nil // not a repository, or no commits yet
	}
	unpushed := map[string]bool{}
	if b, err := git(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		l.Upstream = strings.TrimSpace(string(b))
		if b, err := git(dir, "rev-list", "--left-right", "--count", "@{u}...HEAD"); err == nil {
			if f := strings.Fields(string(b)); len(f) == 2 {
				l.Behind, _ = strconv.Atoi(f[0])
				l.Ahead, _ = strconv.Atoi(f[1])
			}
		}
		if b, err := git(dir, "rev-list", "-n", strconv.Itoa(maxCommits), "@{u}..HEAD"); err == nil {
			for _, s := range strings.Fields(string(b)) {
				unpushed[s] = true
			}
		}
	}
	out, err := git(dir, "log", "-n", strconv.Itoa(maxCommits), "--no-color", "--format=%H%x1f%h%x1f%s%x1f%an%x1f%ct%x1e", "HEAD")
	if err != nil {
		return nil, err
	}
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimSpace(rec), "\x1f")
		if len(f) != 5 {
			continue
		}
		at, _ := strconv.ParseInt(f[4], 10, 64)
		l.Commits = append(l.Commits, Commit{Sha: f[0], Short: f[1], Subject: f[2], Author: f[3], At: at,
			Pushed: l.Upstream != "" && !unpushed[f[0]]})
	}
	return l, nil
}

var hexSha = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// CommitDiff is the message, stat and patch of one commit.
func CommitDiff(dir, sha string) (*Diff, error) {
	if !hexSha.MatchString(sha) { // hex only: never read as an option or a revision expression
		return nil, errors.New("不是一个提交号")
	}
	if _, err := git(dir, "rev-parse", "--verify", "-q", sha+"^{commit}"); err != nil {
		return nil, errors.New("找不到这个提交")
	}
	out, err := git(dir, "show", "--no-ext-diff", "--no-textconv", "--no-color", "-M", "--stat", "--patch",
		"--format=commit %H%nAuthor: %an%nDate:   %ci%n%n%w(0,4,4)%B", sha)
	if err != nil {
		return nil, err
	}
	d := &Diff{Path: sha}
	if len(out) > MaxDiff {
		out, d.Truncated = out[:MaxDiff], true
	}
	d.Text = strings.ToValidUTF8(string(out), "�")
	return d, nil
}
