// Package sysinfo reads how loaded the server is: memory, load, and how much
// memory each tmux session's processes use. Linux only (/proc); elsewhere
// it reports itself unavailable.
package sysinfo

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// Proc is where /proc is; tests point it elsewhere.
var Proc = "/proc"

// SessionMem is the resident memory of one session's process trees.
type SessionMem struct {
	Name  string `json:"name"`
	RSS   int64  `json:"rss"` // bytes
	Procs int    `json:"procs"`
}

// Info is a snapshot of the server's load.
type Info struct {
	Available bool         `json:"available"`
	CPUs      int          `json:"cpus"`
	Load1     float64      `json:"load1"`
	MemTotal  int64        `json:"memTotal"` // bytes
	MemAvail  int64        `json:"memAvail"`
	SwapTotal int64        `json:"swapTotal"`
	SwapFree  int64        `json:"swapFree"`
	HubRSS    int64        `json:"hubRss"`
	Sessions  []SessionMem `json:"sessions"`
}

// Read takes the snapshot; panes maps a session name to its pane pids.
func Read(panes map[string][]int) *Info {
	in := &Info{CPUs: runtime.NumCPU(), Sessions: []SessionMem{}}
	mem, err := os.ReadFile(filepath.Join(Proc, "meminfo"))
	if err != nil {
		return in
	}
	in.Available = true
	for _, l := range strings.Split(string(mem), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		kb, _ := strconv.ParseInt(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			in.MemTotal = kb << 10
		case "MemAvailable:":
			in.MemAvail = kb << 10
		case "SwapTotal:":
			in.SwapTotal = kb << 10
		case "SwapFree:":
			in.SwapFree = kb << 10
		}
	}
	if b, err := os.ReadFile(filepath.Join(Proc, "loadavg")); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			in.Load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	children, rss := procs()
	in.HubRSS = rss[os.Getpid()]
	for name, pids := range panes {
		s := SessionMem{Name: name}
		seen := map[int]bool{}
		stack := append([]int(nil), pids...)
		for len(stack) > 0 && len(seen) < 10000 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if seen[p] {
				continue
			}
			seen[p] = true
			if r, ok := rss[p]; ok {
				s.RSS += r
				s.Procs++
			}
			stack = append(stack, children[p]...)
		}
		in.Sessions = append(in.Sessions, s)
	}
	sort.Slice(in.Sessions, func(i, j int) bool { return in.Sessions[i].RSS > in.Sessions[j].RSS })
	return in
}

// procs reads every process's parent and resident size.
func procs() (children map[int][]int, rss map[int]int64) {
	children, rss = map[int][]int{}, map[int]int64{}
	ents, _ := os.ReadDir(Proc)
	page := int64(os.Getpagesize())
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join(Proc, e.Name(), "stat"))
		if err != nil {
			continue // exited meanwhile
		}
		// "pid (comm) state ppid ...": comm may hold spaces and parentheses
		i := bytes.LastIndexByte(stat, ')')
		if i < 0 {
			continue
		}
		f := strings.Fields(string(stat[i+1:]))
		if len(f) < 2 {
			continue
		}
		ppid, _ := strconv.Atoi(f[1])
		children[ppid] = append(children[ppid], pid)
		if m, err := os.ReadFile(filepath.Join(Proc, e.Name(), "statm")); err == nil {
			if f := strings.Fields(string(m)); len(f) > 1 {
				n, _ := strconv.ParseInt(f[1], 10, 64)
				rss[pid] = n * page
			}
		}
	}
	return children, rss
}
