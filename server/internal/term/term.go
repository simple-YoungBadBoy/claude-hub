// Package term bridges a websocket to `tmux attach` running in a pty.
//
// Client → server: text frames with JSON {"t":"i","d":"<input>"} or
// {"t":"r","c":cols,"r":rows}. Server → client: binary frames of raw output.
package term

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"

	"claudehub/internal/tmux"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type msg struct {
	T string `json:"t"`
	D string `json:"d"`
	C uint16 `json:"c"`
	R uint16 `json:"r"`
}

// MaxConns caps concurrent terminals (each is a `tmux attach` process).
const MaxConns = 24

// ErrTooMany is returned by Acquire when MaxConns terminals are open.
var ErrTooMany = errors.New("打开的终端太多了，先关掉几个标签页")

var (
	liveMu sync.Mutex
	open   int
)

// Acquire reserves a terminal slot; call release exactly once when done.
func Acquire() (release func(), err error) {
	liveMu.Lock()
	defer liveMu.Unlock()
	if open >= MaxConns {
		return nil, ErrTooMany
	}
	open++
	var once sync.Once
	return func() {
		once.Do(func() {
			liveMu.Lock()
			open--
			liveMu.Unlock()
		})
	}, nil
}

// Open is the number of reserved terminal slots.
func Open() int {
	liveMu.Lock()
	defer liveMu.Unlock()
	return open
}

var stops = struct {
	sync.Mutex
	m map[int]func()
	n int
}{m: map[int]func(){}}

// CloseAll stops every `tmux attach` this process started (on shutdown, so
// no orphaned clients stay behind after a restart).
func CloseAll() {
	stops.Lock()
	fns := make([]func(), 0, len(stops.m))
	for _, f := range stops.m {
		fns = append(fns, f)
	}
	stops.Unlock()
	for _, f := range fns {
		f()
	}
}

// Serve attaches to session over ws until either side closes.
func Serve(ws *websocket.Conn, session string, cols, rows uint16) {
	defer ws.Close()
	ws.SetReadLimit(1 << 20) // input and resize messages are tiny
	if cols == 0 || rows == 0 {
		cols, rows = 120, 40
	}
	cmd := tmux.Command("attach-session", "-t", "="+session)
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor", "LANG=C.UTF-8")
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("\r\n[无法连接会话: "+err.Error()+"]\r\n"))
		return
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			f.Close()
			if cmd.Process != nil {
				cmd.Process.Signal(os.Interrupt)
				cmd.Process.Kill()
			}
		})
	}
	stops.Lock()
	stops.n++
	id := stops.n
	stops.m[id] = stop
	stops.Unlock()
	defer func() {
		stop()
		cmd.Wait()
		stops.Lock()
		delete(stops.m, id)
		stops.Unlock()
	}()

	var wmu sync.Mutex
	go func() {
		defer stop()
		buf := make([]byte, 32*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				wmu.Lock()
				werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n])
				wmu.Unlock()
				if werr != nil {
					return
				}
			}
			if err != nil {
				wmu.Lock()
				ws.WriteMessage(websocket.TextMessage, []byte(`{"t":"exit"}`))
				wmu.Unlock()
				ws.Close()
				return
			}
		}
	}()

	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var m msg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.T {
		case "i":
			if _, err := f.Write([]byte(m.D)); err != nil {
				return
			}
		case "r":
			if m.C > 0 && m.R > 0 {
				if err := pty.Setsize(f, &pty.Winsize{Cols: m.C, Rows: m.R}); err != nil {
					log.Printf("resize: %v", err)
				}
			}
		}
	}
}
