# Changelog

## Unreleased

- iPhone: the web UI is now an installable home-screen app (PWA) with a phone layout — bottom tab bar, session drawer, a key bar (Esc / Tab / ⇧Tab / arrows / Ctrl-C / ⇧⏎ / ⏎) and a text box for the phone keyboard. Works over `tailscale serve` HTTPS (websocket origin check accepts the proxied host); the hub skill now uses port 8443 so it doesn't clash with a web server already on 443.
- Fix: copying in Claude Code ("Copied to clipboard") now reaches the Mac/browser clipboard (OSC 52); if the browser refuses, a "点这里复制到本机" button appears.
- Fix: a new conversation shows in the sidebar immediately, even before `claude` has started; requests time out with a clear message instead of hanging.
- tmux sessions started directly on the server are always listed (under "其他 tmux 会话"), no checkbox needed.
- Lower memory risk: the hub caps open terminals, closes its `tmux attach` clients on restart, sets a Go memory limit, and starts a new tmux server in its own systemd scope instead of the hub's.
- New 📊 Usage tab: today's Claude Code tokens on the server (input / output / cache write / cache read, subagent share) by model and project, and the last 7 days.
- Fix: building from a fresh clone failed outside the maintainer's cloud (npm lockfiles pointed at a private mirror).
- Claude Code hooks (optional, `claude-hub hooks install`): exact status per session — thinking, running a tool, waiting for permission, error — instead of reading the screen; each conversation's context usage with a warning at 85%; the pet gets worried when a session waits for you. Without hooks everything falls back to screen reading.
- Fix: redeploying the server could kill every tmux session (including the Claude doing the deploy) when the hub had started the tmux server.
- Fix: the poker table could freeze when chips changed (e.g. a Claude task reward) while a bot was thinking.
- First public version: tmux session list / new / rename / close, xterm.js terminal, image upload pasted as Claude attachments, finish/ask notifications, Texas Hold'em vs 4 bots, pet that grows while Claude works.
- Mac app (Tauri) with a local forwarder so system proxies (Clash etc.) can't break it.
- Claude Code plugin `hub-for-claude-code` with an install/update/troubleshoot skill.
