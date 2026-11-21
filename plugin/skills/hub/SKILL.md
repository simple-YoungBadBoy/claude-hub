---
name: hub
description: Install, update, uninstall or troubleshoot Claude Hub — a Mac app (and web UI) for using the Claude Code sessions that run in tmux on the user's server, over Tailscale, with image upload, a poker table and a pet. Use when the user wants to set up Claude Hub on a server or a Mac, update it, asks why the Claude Hub app/page is blank or cannot connect, or wants to remove it.
---

# Claude Hub

Repo: https://github.com/simple-YoungBadBoy/claude-hub (MIT). Community project, not affiliated with Anthropic.

Two pieces:

- **Server** (Linux with systemd, where `tmux` and `claude` run): Go daemon `claude-hub`, a `systemctl --user` service listening on the machine's **Tailscale IP** and 127.0.0.1, port **7788**. It lists/creates/kills tmux sessions, streams them to the browser (xterm.js), accepts image uploads and pastes their paths into Claude, and hosts the poker table and pet. Data lives in `~/claude-hub/data/` (`token` = login secret).
- **Mac app** (`Claude Hub.app`, Tauri): a thin window onto the server UI. All traffic goes through the server.

Work out which machine you are on (`uname`) and what the user wants, then follow the matching section. Tell the user what you are about to change before changing system settings (services, proxy config, /Applications). Answer in the user's language.

## Install the server (Linux)

1. Check prerequisites and report anything missing instead of guessing:
   `tmux -V`, `claude --version`, `tailscale ip -4`, `go version` (needs ≥ 1.22), `node -v` (≥ 18), `systemctl --user status >/dev/null`.
   Missing Go/Node: suggest the distro packages or the official tarballs; do not install system packages without asking.
2. Get the code: `git clone https://github.com/simple-YoungBadBoy/claude-hub ~/claude-hub` (or `git -C ~/claude-hub pull` if it exists).
3. `~/claude-hub/deploy/install.sh` — builds web + server, installs and (re)starts the user service.
4. For the service to survive logout: `loginctl show-user "$USER" -p Linger`; if `no`, ask before running `sudo loginctl enable-linger "$USER"`.
5. Verify: `~/claude-hub/scripts/live-check.sh` must print `LIVE CHECK OK`.
6. **Hooks (recommended, ask first).** Explain: Claude Code will run `~/claude-hub/bin/claude-hub hook` on each event (prompt, tool use, permission request, stop…); it forwards only the event name, tool name and session id to the local hub — never prompts or tool inputs — and always exits 0 within a second. This gives exact status (thinking / running a tool / waiting for permission / error) and each conversation's context usage. Changing `~/.claude/settings.json` is the user's call: have them run it themselves (in Claude Code: `! ~/claude-hub/bin/claude-hub hooks install`). It backs up the file and keeps every other setting. Running sessions pick hooks up after `claude` is restarted. Remove with `claude-hub hooks uninstall`; check with `claude-hub hooks status`.
7. Give the user: the URL `http://<tailscale hostname or IP>:7788` (`tailscale status --self` / `tailscale ip -4`) and the token from `~/claude-hub/data/token`. They enter the token once in the app or browser.

## Install the Mac app (macOS)

Prefer the prebuilt app:

```bash
cd "$(mktemp -d)"
curl -fL -o hub.zip https://github.com/simple-YoungBadBoy/claude-hub/releases/latest/download/Claude-Hub-macos.zip
unzip -q hub.zip
osascript -e 'quit app "Claude Hub"' 2>/dev/null; rm -rf "/Applications/Claude Hub.app"
mv "Claude Hub.app" /Applications/
xattr -dr com.apple.quarantine "/Applications/Claude Hub.app"   # unsigned build
open "/Applications/Claude Hub.app"
```

If there is no release yet, or the user wants to build: needs Xcode command line tools and Rust (`rustup`). Then
`git clone https://github.com/simple-YoungBadBoy/claude-hub ~/claude-hub && cd ~/claude-hub/mac && cargo install tauri-cli --version '^2' --locked && cargo tauri icon app-icon.png && cargo tauri build --bundles app`, copy `src-tauri/target/release/bundle/macos/Claude Hub.app` to /Applications.

First launch asks for the server URL (`http://<server>:7788`), then the token. Menu "Claude Hub → 更改服务器…" (⌘,) changes the server later.

Check the Mac can reach the server: `curl -s -o /dev/null -w '%{http_code}\n' http://<server>:7788/` → `200`.

## Update

- Server: `git -C ~/claude-hub pull && ~/claude-hub/deploy/install.sh && ~/claude-hub/scripts/live-check.sh`. The UI is served by the server, so the Mac app picks it up with ⌘R — no reinstall.
- Mac app: only when the release notes say the app changed — repeat "Install the Mac app".

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Browser page blank or "can't connect", but `curl` works | A **system proxy** (Clash, Surge, Shadowrocket…) sends 100.x / `*.ts.net` traffic out to the internet. Add `100.64.0.0/10,*.ts.net` to the proxy's bypass list (Clash Verge: Settings → System Proxy → bypass; config key `system_proxy_bypass` in `verge.yaml`). Ask first, back the config up, then restart the proxy app. The Mac app itself is not affected: it forwards through `127.0.0.1:17788`. |
| App shows "连不上服务器" page | Tailscale off on the Mac, server down (`systemctl --user status claude-hub`, `journalctl --user -u claude-hub -n 50`), or wrong URL (⌘, to change). |
| Login says token wrong | Token is `~/claude-hub/data/token` on the server (whitespace is trimmed). |
| Sessions missing from the list | Only panes running `claude` are shown by default; tick "显示其他 tmux 会话". The service must run as the same user as your tmux. |
| Status wrong / no context bar | Without hooks, status is read from the screen (`esc to interrupt`) and there is no context usage. Check `~/claude-hub/bin/claude-hub hooks status` (should be 11/11) and restart `claude` in that session. |
| Image not attached | The path is pasted as a bracketed paste; Claude Code shows `[Image #N]`. Uploads are stored in `~/claude-hub/data/uploads/<session>/`. |

## Uninstall

Server: `~/claude-hub/bin/claude-hub hooks uninstall` (only removes our entries), then `systemctl --user disable --now claude-hub && rm ~/.config/systemd/user/claude-hub.service` (keep or delete `~/claude-hub`; `data/` holds chips/pet/uploads).
Mac: delete `/Applications/Claude Hub.app` and `~/Library/Application Support/dev.claudehub.mac`.
