package sysinfo

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRead(t *testing.T) {
	root := t.TempDir()
	old := Proc
	Proc = root
	defer func() { Proc = old }()
	w := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o700)
		os.WriteFile(filepath.Join(root, p), []byte(s), 0o600)
	}
	w("meminfo", "MemTotal:        4000000 kB\nMemFree:  1 kB\nMemAvailable:    1000000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	w("loadavg", "1.50 0.80 0.40 2/300 12345\n")
	page := int64(os.Getpagesize())
	proc := func(pid, ppid, pages int, comm string) {
		w(strconv.Itoa(pid)+"/stat", strconv.Itoa(pid)+" ("+comm+") S "+strconv.Itoa(ppid)+" 1 1 0")
		w(strconv.Itoa(pid)+"/statm", "100 "+strconv.Itoa(pages)+" 0 0 0 0 0")
	}
	proc(10, 1, 100, "bash")
	proc(11, 10, 1000, "claude) S 99 (x") // a hostile name must not fool the parser
	proc(12, 11, 50, "node")
	proc(20, 1, 7, "zsh")
	proc(30, 1, 5000, "other")
	in := Read(map[string][]int{"a": {10}, "b": {20}, "gone": {999}})
	if !in.Available || in.MemTotal != 4000000<<10 || in.MemAvail != 1000000<<10 || in.Load1 != 1.5 {
		t.Fatalf("%+v", in)
	}
	if len(in.Sessions) != 3 || in.Sessions[0].Name != "a" || in.Sessions[0].RSS != 1150*page || in.Sessions[0].Procs != 3 ||
		in.Sessions[1].RSS != 7*page || in.Sessions[2].RSS != 0 {
		t.Fatalf("%+v", in.Sessions)
	}
	Proc = filepath.Join(root, "missing")
	if in := Read(nil); in.Available {
		t.Fatal("no /proc")
	}
}
