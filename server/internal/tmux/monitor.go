package tmux

import (
	"log"
	"os"
	"reflect"
	"sync"
	"time"
)

// Monitor polls tmux, tracks Claude status per session and reports changes.
type Monitor struct {
	mu       sync.Mutex
	sessions []Session
	status   map[string]string
	grace    map[string]int // consecutive non-working polls while still reported as working
	guess    map[string]*guessed

	// Hooks, when set, refines status with Claude Code hook events.
	Hooks *HookTracker

	// OnList is called whenever the session list (or a status) changes.
	OnList func([]Session)
	// OnTransition is called when a claude session changes status.
	OnTransition func(name, from, to string)
	// OnTick is called every minute with the number of working sessions.
	OnTick func(working int)
}

// guessed is the summary of a session without hooks, from the newest
// transcript in its project directory (looked up every ~10s).
type guessed struct {
	path, key string
	looked    time.Time
	sum       *Summary
}

func NewMonitor() *Monitor {
	return &Monitor{status: map[string]string{}, grace: map[string]int{}, guess: map[string]*guessed{}}
}

// guessSummary is called with m.mu held.
func (m *Monitor) guessSummary(s *Session) *Summary {
	g := m.guess[s.Name]
	if g == nil {
		g = &guessed{}
		m.guess[s.Name] = g
	}
	if time.Since(g.looked) > 10*time.Second {
		g.looked = time.Now()
		if p := LatestTranscript(ProjectsDir(), s.Path); p != g.path {
			g.path, g.key, g.sum = p, "", nil
		}
	}
	if g.path == "" {
		return nil
	}
	if fi, err := os.Stat(g.path); err == nil {
		if key := fi.ModTime().String() + "/" + itoa(fi.Size()); key != g.key {
			g.key = key
			_, sum := ReadTranscript(g.path)
			g.sum = keepTitle(sum, g.sum)
		}
	}
	return g.sum
}

// Sessions returns the latest snapshot.
func (m *Monitor) Sessions() []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Session, len(m.sessions))
	copy(out, m.sessions)
	return out
}

// Poke refreshes immediately (after create/kill).
func (m *Monitor) Poke() { m.poll() }

func (m *Monitor) Run(interval time.Duration) {
	m.poll()
	t := time.NewTicker(interval)
	lastMinute := time.Now()
	for range t.C {
		m.poll()
		if time.Since(lastMinute) >= time.Minute {
			lastMinute = time.Now()
			if m.OnTick != nil {
				n := 0
				for _, s := range m.Sessions() {
					if s.Status == "working" {
						n++
					}
				}
				m.OnTick(n)
			}
		}
	}
}

func (m *Monitor) poll() {
	list, err := List()
	if err != nil {
		log.Printf("tmux list: %v", err)
		return
	}
	type tr struct{ name, from, to string }
	var trs []tr
	m.mu.Lock()
	seen := map[string]bool{}
	for i := range list {
		s := &list[i]
		if !s.Claude {
			continue
		}
		seen[s.Name] = true
		scr, err := Capture(s.Name)
		if err != nil {
			continue
		}
		s.Status = Status(scr)
		prev, ok := m.status[s.Name]
		if m.Hooks != nil {
			if st, act, ctx, hooked := m.Hooks.resolve(s.PaneID, s.Status); hooked {
				s.Status, s.Activity, s.Context, s.Hooked = st, act, ctx, true
			}
		}
		if s.Hooked {
			s.Summary = m.Hooks.summary(s.PaneID)
		} else {
			s.Summary = m.guessSummary(s)
		}
		// The spinner can vanish for a moment between tool calls; only leave
		// "working" after it has been gone for two polls in a row.
		if !s.Hooked && ok && prev == "working" && s.Status == "idle" && m.grace[s.Name] < 1 {
			m.grace[s.Name]++
			s.Status = "working"
		} else {
			m.grace[s.Name] = 0
		}
		if s.Status == "waiting" {
			s.Prompt = ParsePrompt(scr)
		}
		if ok && prev != s.Status {
			trs = append(trs, tr{s.Name, prev, s.Status})
		}
		m.status[s.Name] = s.Status
	}
	for n := range m.status {
		if !seen[n] {
			delete(m.status, n)
			delete(m.grace, n)
			delete(m.guess, n)
		}
	}
	if m.Hooks != nil {
		alive := map[string]bool{}
		for _, s := range list {
			alive[s.PaneID] = true
		}
		m.Hooks.prune(alive)
	}
	changed := !reflect.DeepEqual(list, m.sessions)
	m.sessions = list
	m.mu.Unlock()

	if changed && m.OnList != nil {
		m.OnList(list)
	}
	if m.OnTransition != nil {
		for _, t := range trs {
			m.OnTransition(t.name, t.from, t.to)
		}
	}
}
