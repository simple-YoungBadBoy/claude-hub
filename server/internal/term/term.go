// Package term bridges a websocket to `tmux attach` running in a pty.
//
// Client → server: text frames with JSON {"t":"i","d":"<input>"} or
// {"t":"r","c":cols,"r":rows}. Server → client: binary frames of raw output.
package term

import (
	"encoding/json"
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

// Serve attaches to session over ws until either side closes.
func Serve(ws *websocket.Conn, session string, cols, rows uint16) {
	defer ws.Close()
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
	defer func() {
		stop()
		cmd.Wait()
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
