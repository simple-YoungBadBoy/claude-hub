---
name: hub
description: Install, update, uninstall or troubleshoot Claude Hub — a Mac app (and web UI) for using the Claude Code sessions that run in tmux on the user's server, over Tailscale, with image upload, a poker table and a pet. Use when the user wants to set up Claude Hub on a server or a Mac, update it, asks why the Claude Hub app/page is blank or cannot connect, or wants to remove it.
---

# Claude Hub

Repo: https://github.com/simple-YoungBadBoy/claude-hub (MIT). Community project, not affiliated with Anthropic.

Two pieces:

- **Server** (Linux with systemd, where `tmux` and `claude` run): Go daemon `claude-hub`, a `systemctl --user` service listening on the machine's **Tailscale IP** and 127.0.0.1, port **7788**. It lists/creates/kills tmux sessions, streams them to the browser (xterm.js), accepts image uploads and pastes their paths into Claude, and hosts the poker table and pet. Data lives in `~/claude-hub/data/` (`token` = login secret).
- **Mac app** (`Claude Hub.app`, Tauri): a thin window onto the server UI. All traffic goes through the server.
- **iPhone / iPad**: the same web UI as a home-screen web app (PWA). No App Store app.

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

## Use on iPhone (PWA)

1. On the iPhone: install the Tailscale app and sign in to the same tailnet.
2. HTTPS (recommended — enables the clipboard API, a proper full-screen web app and, later, push notifications). It stays inside the tailnet; **never use `tailscale funnel`**, which would publish the hub on the internet.
   - The user enables **MagicDNS** and **HTTPS Certificates** in the Tailscale admin console (https://login.tailscale.com/admin/dns). Check with `tailscale status --json | python3 -c 'import json,sys; print(json.load(sys.stdin).get("CertDomains"))'` — it must list `<machine>.<tailnet>.ts.net`.
   - If `tailscale serve` says "Serve is not enabled on your tailnet", it prints a `login.tailscale.com/f/serve?...` link: give it to the user to click **Enable** (only they can), then run serve again.
   - On the server, after asking: `sudo tailscale serve --bg --https=8443 http://127.0.0.1:7788` (persists across reboots; `tailscale serve status` shows the URL). Port 8443 avoids clashing with a web server (Caddy/nginx) already on 443; plain `--bg` (port 443) is fine when nothing else uses it. Without sudo the user can first run `sudo tailscale set --operator=$USER`.
   - URL: `https://<machine>.<tailnet>.ts.net:8443`. The very first request can take ~30 s while the certificate is issued.
   - Without HTTPS, `http://<tailscale IP>:7788` works too (copy then needs a tap on the "点这里复制" button).
3. In Safari open the URL, log in with the token, then Share → **Add to Home Screen**.
4. If the phone runs a proxy/VPN-style app besides Tailscale, it must bypass `100.64.0.0/10` and `*.ts.net` (same as Clash on the Mac).

## Update

- Server: `git -C ~/claude-hub pull && ~/claude-hub/deploy/install.sh && ~/claude-hub/scripts/live-check.sh`. The UI is served by the server, so the Mac app picks it up with ⌘R — no reinstall.
- Mac app: only when the release notes say the app changed — repeat "Install the Mac app".

## Troubleshooting

| Symptom | Cause / fix |
|---|---|
| Browser page blank or "can't connect", but `curl` works | A **system proxy** (Clash, Surge, Shadowrocket…) sends 100.x / `*.ts.net` traffic out to the internet. Add `100.64.0.0/10,*.ts.net` to the proxy's bypass list (Clash Verge: Settings → System Proxy → bypass; config key `system_proxy_bypass` in `verge.yaml`). Ask first, back the config up, then restart the proxy app. The Mac app itself is not affected: it forwards through `127.0.0.1:17788`. |
| iPhone page blank / can't connect | Tailscale on the phone must be connected and signed in to the same account (the phone shows up in `tailscale status` on the server); for HTTPS check `tailscale serve status` on the server and that the tailnet has HTTPS certificates enabled. |
| App shows "连不上服务器" page | Tailscale off on the Mac, server down (`systemctl --user status claude-hub`, `journalctl --user -u claude-hub -n 50`), or wrong URL (⌘, to change). |
| Login says token wrong | Token is `~/claude-hub/data/token` on the server (whitespace is trimmed). |
| Sessions missing from the list | Sessions not running `claude` are listed under "其他 tmux 会话". Only the default tmux server is scanned (not `tmux -L <name>` sockets), and the service must run as the same user as your tmux. |
| "Copied" in Claude but nothing on the Mac | Claude copies with `tmux load-buffer -w`, which goes to the most recently active client of that session. If an SSH terminal is also attached, it may get it instead; otherwise click the "点这里复制到本机" button that appears in the terminal. Option+drag selects terminal text directly (then ⌘C). |
| Status wrong / no context bar | Without hooks, status is read from the screen (`esc to interrupt`) and there is no context usage. Check `~/claude-hub/bin/claude-hub hooks status` (should be 11/11) and restart `claude` in that session. |
| Image not attached | The path is pasted as a bracketed paste; Claude Code shows `[Image #N]`. Uploads are stored in `~/claude-hub/data/uploads/<session>/`. |

## Uninstall

Server: `~/claude-hub/bin/claude-hub hooks uninstall` (only removes our entries), then `systemctl --user disable --now claude-hub && rm ~/.config/systemd/user/claude-hub.service` (keep or delete `~/claude-hub`; `data/` holds chips/pet/uploads).
Mac: delete `/Applications/Claude Hub.app` and `~/Library/Application Support/dev.claudehub.mac`.
