# Changelog

## Unreleased

- New look options: a light "paper" theme (☀️ / 🌙 in the top bar), sessions grouped by project folder with a ＋ to start another one there, sessions needing you sorted first, and how long each has been running.
- On wide screens the active terminal stays docked on the right while you play poker or check the pet / usage (▥ to hide it).
- Input bar ⚡: one tap for /compact, /clear, /model, /effort, /context, /usage, /resume, /init.
- ⌘1–9 (or Ctrl+1–9) jumps to a session.
- New session: "接着这个目录上次的对话" starts `claude --continue`.
- Fix: after renaming the open session the terminal no longer disappears.
- iPhone lock-screen notifications (Web Push): in the home-screen app opened over HTTPS, tap "🔔 开启锁屏通知" in the session list. You get "finished / needs you / error" with the session's title — not while you're looking at an open page. Payloads are end-to-end encrypted; only browser push services are allowed.
- Typing: an input bar is now on by default on Mac and phone — text appears instantly (no round trip to the server), the native IME works normally, Enter sends (Shift+Enter for a newline, Enter while picking IME candidates never sends), and with an empty bar ↑↓ / Tab / Enter go to Claude; Esc and Ctrl-C interrupt anytime. Toggle with "⌨ 输入栏".
- Sending is reliable: the server types the message, waits until it shows in Claude's input, then presses Enter; `/commands` still run, multi-line text keeps its newlines.
- Esc reacts at once: the hub lowers tmux's `escape-time` from the 500 ms default to 10 ms (runtime only, your own setting is respected; `-tmux-escape-time=-1` to opt out).
- Terminal upgraded to xterm.js 6.0 (fixes doubled characters with some IMEs).
- One-tap approval: when Claude asks for permission (or asks a numbered question), a card under the terminal shows the question, the command and the options — tap one instead of typing. Works with or without hooks.
- Session list shows what each conversation is about (Claude Code's own title) and, when idle, Claude's last words.
- ✎ text box: 🕘 recent messages to resend, a hint that the keyboard's dictation works, and no more iOS zoom when it gets focus.
- Fix (iPhone): with Sogou and other third-party keyboards, inserting text in the middle of the ✎ box after sending a message no longer deletes earlier characters.
- Fix (iPhone): Chinese input with third-party keyboards (Sogou, WeChat…) in the ✎ text box no longer gets wiped by background updates before it is committed.
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
