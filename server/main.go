// claude-hub: serve tmux-hosted Claude Code sessions, a poker table and a pet
// to the Mac client over Tailscale.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"claudehub/internal/hooks"
	"claudehub/internal/hub"
	"claudehub/internal/pet"
	"claudehub/internal/poker"
	"claudehub/internal/store"
	"claudehub/internal/term"
	"claudehub/internal/tmux"

	"github.com/gorilla/websocket"
)

//go:embed all:webdist
var webdist embed.FS

const cookieName = "claudehub"

// Version is set at build time: -ldflags "-X main.Version=v0.2.0".
var Version = "dev"

type server struct {
	dataDir string
	token   string
	command string
	hub     *hub.Hub
	mon     *tmux.Monitor
	table   *poker.Table
	pet     *pet.Pet
	up      websocket.Upgrader
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "hook": // called by Claude Code on every hook event
			hooks.RunClient()
			return
		case "hooks":
			hooksCmd(os.Args[2:])
			return
		}
	}
	home, _ := os.UserHomeDir()
	addrs := flag.String("addr", "", "comma-separated listen addresses (default: <tailscale ip>:7788,127.0.0.1:7788)")
	data := flag.String("data", filepath.Join(home, "claude-hub", "data"), "data directory")
	command := flag.String("cmd", "claude", "command run in new sessions")
	socket := flag.String("tmux-socket", "", "use a private tmux server (tmux -L NAME); for tests")
	version := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *version {
		fmt.Println(Version)
		return
	}
	tmux.Socket = *socket

	s := &server{dataDir: *data, command: *command, hub: hub.New(), mon: tmux.NewMonitor()}
	s.mon.Hooks = tmux.NewHookTracker()
	if err := os.MkdirAll(s.dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	s.token = loadToken(filepath.Join(s.dataDir, "token"))

	var saved poker.Saved
	if err := store.Load(s.path("poker.json"), &saved); err != nil {
		log.Printf("poker.json: %v", err)
	}
	s.table = poker.NewTable(saved)
	s.table.Persist = func(v poker.Saved) { logErr(store.Save(s.path("poker.json"), v)) }
	s.table.OnChange = func(v poker.View) { s.hub.Publish("poker", v) }

	var ps pet.State
	if err := store.Load(s.path("pet.json"), &ps); err != nil {
		log.Printf("pet.json: %v", err)
	}
	s.pet = pet.New(ps, s.table)
	s.pet.Persist = func(v pet.State) { logErr(store.Save(s.path("pet.json"), v)) }
	s.pet.OnChange = func(v pet.State) { s.hub.Publish("pet", v) }

	s.mon.OnList = func(l []tmux.Session) { s.hub.Publish("sessions", l) }
	s.mon.OnTransition = func(name, from, to string) {
		s.hub.Publish("status", map[string]string{"name": name, "from": from, "to": to})
		switch {
		case from == "working" && to == "idle":
			s.pet.TaskDone(name)
		case to == "waiting":
			s.pet.Say(name + " 在等你确认！")
		case to == "error":
			s.pet.Say("呜… " + name + " 出错了")
		}
	}
	s.mon.OnTick = s.pet.Tick
	go s.mon.Run(2 * time.Second)

	mux := http.NewServeMux()
	s.routes(mux)

	list := listenAddrs(*addrs)
	errc := make(chan error, len(list))
	for _, a := range list {
		a := a
		go func() {
			srv := &http.Server{Addr: a, Handler: logRequests(mux), ReadHeaderTimeout: 10 * time.Second}
			log.Printf("listening on http://%s", a)
			errc <- srv.ListenAndServe()
		}()
	}
	log.Printf("token file: %s", filepath.Join(s.dataDir, "token"))
	log.Fatal(<-errc)
}

func logErr(err error) {
	if err != nil {
		log.Print(err)
	}
}

func (s *server) path(name string) string { return filepath.Join(s.dataDir, name) }

func loadToken(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
		return strings.TrimSpace(string(b))
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		log.Fatal(err)
	}
	tok := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(tok+"\n"), 0o600); err != nil {
		log.Fatal(err)
	}
	return tok
}

func listenAddrs(flagVal string) []string {
	if flagVal != "" {
		return strings.Split(flagVal, ",")
	}
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	ip := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if err != nil || net.ParseIP(ip) == nil {
		log.Fatalf("找不到 Tailscale IP（%v），用 -addr 指定", err)
	}
	return []string{ip + ":7788", "127.0.0.1:7788"}
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet {
			log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		}
		h.ServeHTTP(w, r)
	})
}

// ---- auth ----

func (s *server) authed(r *http.Request) bool {
	tok := ""
	if c, err := r.Cookie(cookieName); err == nil {
		tok = c.Value
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimPrefix(h, "Bearer ")
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) == 1
}

// api wraps a JSON handler: auth, a custom header on writes (blocks
// cross-site form posts), JSON in/out.
func (s *server) api(fn func(r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Hub") != "1" {
			http.Error(w, `{"error":"missing X-Hub header"}`, http.StatusForbidden)
			return
		}
		v, err := fn(r)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if v == nil {
			v = map[string]bool{"ok": true}
		}
		json.NewEncoder(w).Encode(v)
	}
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("bad json: %w", err)
	}
	return nil
}

// ---- routes ----

func (s *server) routes(mux *http.ServeMux) {
	sub, _ := fs.Sub(webdist, "webdist")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		static.ServeHTTP(w, r)
	})

	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Token string }
		decode(r, &body)
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(body.Token)), []byte(s.token)) != 1 {
			time.Sleep(500 * time.Millisecond)
			http.Error(w, `{"error":"token 不对"}`, http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteLaxMode, MaxAge: 365 * 24 * 3600})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	mux.HandleFunc("GET /api/state", s.api(func(r *http.Request) (any, error) { return s.state(), nil }))
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"version": Version})
	})

	mux.HandleFunc("POST /api/sessions", s.api(func(r *http.Request) (any, error) {
		var body struct{ Name, Dir string }
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		dir := expandHome(strings.TrimSpace(body.Dir))
		name, err := tmux.Create(body.Name, dir, s.command)
		if err != nil {
			return nil, err
		}
		s.rememberDir(dir)
		s.mon.Poke()
		return map[string]string{"name": name}, nil
	}))
	mux.HandleFunc("DELETE /api/sessions/{name}", s.api(func(r *http.Request) (any, error) {
		err := tmux.Kill(r.PathValue("name"))
		s.mon.Poke()
		return nil, err
	}))
	mux.HandleFunc("POST /api/sessions/{name}/rename", s.api(func(r *http.Request) (any, error) {
		var body struct{ Name string }
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		n, err := tmux.Rename(r.PathValue("name"), body.Name)
		s.mon.Poke()
		return map[string]string{"name": n}, err
	}))
	mux.HandleFunc("POST /api/sessions/{name}/paste", s.api(func(r *http.Request) (any, error) {
		var body struct{ Text string }
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		return nil, tmux.Paste(r.PathValue("name"), body.Text)
	}))
	mux.HandleFunc("POST /api/upload/{name}", s.api(s.upload))

	mux.HandleFunc("GET /ws/term/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !s.authed(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		name := r.PathValue("name")
		if !tmux.Exists(name) {
			http.Error(w, "no such session", http.StatusNotFound)
			return
		}
		ws, err := s.up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c, _ := strconv.Atoi(r.URL.Query().Get("c"))
		rw, _ := strconv.Atoi(r.URL.Query().Get("r"))
		term.Serve(ws, name, uint16(c), uint16(rw))
	})
	mux.HandleFunc("GET /ws/events", s.events)
	mux.HandleFunc("POST /api/hook", s.api(func(r *http.Request) (any, error) {
		// only the local `claude-hub hook` client posts here
		if host, _, _ := net.SplitHostPort(r.RemoteAddr); !net.ParseIP(host).IsLoopback() {
			return nil, errors.New("hooks are accepted from localhost only")
		}
		var e tmux.HookEvent
		if err := decode(r, &e); err != nil {
			return nil, err
		}
		s.mon.Hooks.Apply(e)
		go s.mon.Poke()
		return nil, nil
	}))

	// poker
	mux.HandleFunc("POST /api/poker/start", s.api(func(r *http.Request) (any, error) { return nil, s.table.StartHand() }))
	mux.HandleFunc("POST /api/poker/relief", s.api(func(r *http.Request) (any, error) { return nil, s.table.Relief() }))
	mux.HandleFunc("POST /api/poker/act", s.api(func(r *http.Request) (any, error) {
		var body struct {
			Action string
			Amount int
		}
		if err := decode(r, &body); err != nil {
			return nil, err
		}
		return nil, s.table.Act(body.Action, body.Amount)
	}))

	// pet
	type petBody struct{ ID, Name string }
	petAction := func(fn func(b petBody) (pet.State, error)) http.HandlerFunc {
		return s.api(func(r *http.Request) (any, error) {
			var b petBody
			if err := decode(r, &b); err != nil {
				return nil, err
			}
			return fn(b)
		})
	}
	mux.HandleFunc("POST /api/pet/feed", petAction(func(b petBody) (pet.State, error) { return s.pet.Feed(b.ID) }))
	mux.HandleFunc("POST /api/pet/play", petAction(func(b petBody) (pet.State, error) { return s.pet.Play() }))
	mux.HandleFunc("POST /api/pet/pat", petAction(func(b petBody) (pet.State, error) { return s.pet.Pat(), nil }))
	mux.HandleFunc("POST /api/pet/buy", petAction(func(b petBody) (pet.State, error) { return s.pet.BuyHat(b.ID) }))
	mux.HandleFunc("POST /api/pet/wear", petAction(func(b petBody) (pet.State, error) { return s.pet.Wear(b.ID) }))
	mux.HandleFunc("POST /api/pet/rename", petAction(func(b petBody) (pet.State, error) { return s.pet.Rename(b.Name) }))
}

func (s *server) state() map[string]any {
	return map[string]any{
		"sessions": s.mon.Sessions(),
		"poker":    s.table.View(),
		"pet":      s.pet.Get(),
		"foods":    pet.Foods,
		"hats":     pet.Hats,
		"dirs":     s.dirs(),
		"hooks":    s.hooksInfo(),
	}
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ch, cancel := s.hub.Subscribe()
	defer cancel()
	init, _ := json.Marshal(map[string]any{"type": "state", "data": s.state()})
	if ws.WriteMessage(websocket.TextMessage, init) != nil {
		return
	}
	// reader: detect close
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-done:
			return
		case b, ok := <-ch:
			if !ok || ws.WriteMessage(websocket.TextMessage, b) != nil {
				return
			}
		case <-ping.C:
			if ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)) != nil {
				return
			}
		}
	}
}

// ---- hooks ----

func (s *server) hooksInfo() map[string]any {
	n, total := hooks.Installed(hooks.SettingsPath())
	last := s.mon.Hooks.LastEvent()
	info := map[string]any{"installed": n, "total": total}
	if !last.IsZero() {
		info["lastEvent"] = last.Unix()
	}
	return info
}

func hooksCmd(args []string) {
	path := hooks.SettingsPath()
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "install":
		cmd, err := hooks.Command()
		if err != nil {
			log.Fatal(err)
		}
		backup, err := hooks.Install(path, cmd)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("已在 %s 安装 Claude Hub hooks（%d 个事件）\n命令：%s\n", path, len(hooks.Events), cmd)
		if backup != "" {
			fmt.Printf("原文件备份：%s\n", backup)
		}
		fmt.Println("新开的 Claude 会话立即生效；已在运行的会话需要重启 claude 才会加载。")
	case "uninstall":
		backup, err := hooks.Uninstall(path)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("已从 %s 移除 Claude Hub hooks（其他设置未动）\n", path)
		if backup != "" {
			fmt.Printf("原文件备份：%s\n", backup)
		}
	case "status":
		n, total := hooks.Installed(path)
		fmt.Printf("%s: Claude Hub hooks %d/%d\n", path, n, total)
	default:
		fmt.Println("用法: claude-hub hooks install|uninstall|status")
		os.Exit(2)
	}
}

// ---- uploads ----

var unsafeChars = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

func (s *server) upload(r *http.Request) (any, error) {
	name := r.PathValue("name")
	if !tmux.Exists(name) {
		return nil, errors.New("会话不存在")
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 30<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return nil, fmt.Errorf("上传失败（最大 30MB）: %w", err)
	}
	defer r.MultipartForm.RemoveAll()
	var paths []string
	for _, fh := range r.MultipartForm.File["file"] {
		f, err := fh.Open()
		if err != nil {
			return nil, err
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		ext := strings.ToLower(filepath.Ext(fh.Filename))
		switch http.DetectContentType(head[:n]) {
		case "image/png":
			ext = ".png"
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		}
		base := strings.TrimSuffix(filepath.Base(fh.Filename), filepath.Ext(fh.Filename))
		base = strings.Trim(unsafeChars.ReplaceAllString(base, "-"), "-.")
		if base == "" || base == "image" || len(base) > 40 {
			base = "img"
		}
		dir := filepath.Join(s.dataDir, "uploads", tmux.SanitizeName(name))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			f.Close()
			return nil, err
		}
		dst := filepath.Join(dir, fmt.Sprintf("%s-%s%s", time.Now().Format("0102-150405.000"), base, ext))
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			f.Close()
			return nil, err
		}
		_, err = io.Copy(out, io.MultiReader(strings.NewReader(string(head[:n])), f))
		out.Close()
		f.Close()
		if err != nil {
			return nil, err
		}
		paths = append(paths, dst)
	}
	if len(paths) == 0 {
		return nil, errors.New("没有文件")
	}
	if err := tmux.Paste(name, strings.Join(paths, " ")+" "); err != nil {
		return nil, err
	}
	return map[string]any{"paths": paths}, nil
}

// ---- recent directories ----

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

func (s *server) rememberDir(dir string) {
	if dir == "" {
		return
	}
	var dirs []string
	store.Load(s.path("dirs.json"), &dirs)
	out := []string{dir}
	for _, d := range dirs {
		if d != dir && len(out) < 12 {
			out = append(out, d)
		}
	}
	logErr(store.Save(s.path("dirs.json"), out))
}

// dirs suggests working directories: recent ones, then those of live sessions.
func (s *server) dirs() []string {
	var dirs []string
	store.Load(s.path("dirs.json"), &dirs)
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, d := range dirs {
		add(d)
	}
	for _, ss := range s.mon.Sessions() {
		add(ss.Path)
	}
	home, _ := os.UserHomeDir()
	add(home)
	return out
}
