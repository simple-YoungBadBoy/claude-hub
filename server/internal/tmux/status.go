package tmux

import (
	"regexp"
	"strconv"
	"strings"
)

// Status classifies a Claude Code screen:
//
//	working — the spinner line shows "esc to interrupt"
//	waiting — a permission / choice prompt is open
//	idle    — anything else
func Status(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > 25 {
		lines = lines[len(lines)-25:]
	}
	tail := strings.ToLower(strings.Join(lines, "\n"))
	switch {
	case strings.Contains(tail, "esc to interrupt"):
		return "working"
	case strings.Contains(tail, "do you want to"),
		strings.Contains(tail, "❯ 1. yes"),
		strings.Contains(tail, "esc to cancel"):
		return "waiting"
	default:
		return "idle"
	}
}

// Prompt is an open choice in Claude Code (permission request, question):
// pressing an option's number picks it.
type Prompt struct {
	Question string   `json:"question"`
	Detail   []string `json:"detail,omitempty"` // e.g. the command to approve
	Options  []Option `json:"options"`
}

type Option struct {
	Key   string `json:"key"` // "1".."9"
	Label string `json:"label"`
}

var optionRe = regexp.MustCompile(`^(\s*)(?:[❯>›]\s*)?([1-9])\.\s+(\S.*)$`)

// ParsePrompt finds a numbered choice at the bottom of a Claude Code screen.
func ParsePrompt(screen string) *Prompt {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	for i, l := range lines {
		l = strings.TrimRight(l, " ")
		if t := strings.TrimLeft(l, " "); strings.HasPrefix(t, "│") { // boxed prompts
			l = strings.Repeat(" ", len(l)-len(t)) + " " + strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(t, "│"), "│"), " ")
		}
		lines[i] = l
	}
	var best *Prompt
	bestEnd := -1
	for i := 0; i < len(lines); i++ {
		m := optionRe.FindStringSubmatch(lines[i])
		if m == nil || m[2] != "1" {
			continue
		}
		p := &Prompt{}
		indent := len(m[1])
		j := i
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.TrimSpace(l) == "" {
				break
			}
			if m := optionRe.FindStringSubmatch(l); m != nil && len(m[1]) <= indent+2 {
				if m[2] != strconv.Itoa(len(p.Options)+1) {
					break
				}
				p.Options = append(p.Options, Option{Key: m[2], Label: strings.TrimSpace(m[3])})
				continue
			}
			lead := len(l) - len(strings.TrimLeft(l, " "))
			if len(p.Options) > 0 && lead > indent+2 { // a wrapped label
				o := &p.Options[len(p.Options)-1]
				o.Label += " " + strings.TrimSpace(l)
				continue
			}
			break
		}
		if len(p.Options) < 2 {
			continue
		}
		// question: the nearest line above; detail: what's above that, up to a rule
		k := i - 1
		for k >= 0 && strings.TrimSpace(lines[k]) == "" {
			k--
		}
		if k < 0 {
			continue
		}
		p.Question = strings.TrimSpace(lines[k])
		var detail []string
		for k--; k >= 0 && len(detail) < 6; k-- {
			t := strings.TrimSpace(lines[k])
			if strings.HasPrefix(t, "──") || strings.HasPrefix(t, "╭") || strings.HasPrefix(t, "❯ ") {
				break
			}
			if t == "" || strings.HasPrefix(t, "╌") || strings.HasPrefix(t, "Tip:") {
				continue
			}
			detail = append([]string{t}, detail...)
		}
		p.Detail = detail
		best, bestEnd = p, j
		i = j
	}
	if best == nil {
		return nil
	}
	// must be the last thing on screen (only a footer like "Esc to cancel" below)
	rest := 0
	for _, l := range lines[bestEnd:] {
		if strings.TrimSpace(l) != "" {
			rest++
		}
	}
	if rest > 3 {
		return nil
	}
	return best
}
