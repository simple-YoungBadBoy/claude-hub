#!/usr/bin/env bash
# Build, install and launch the Mac app on a test Mac over SSH, then verify it
# actually connects to this server. Needed whenever mac/ changed.
#   MAC_HOST=user@macbook.tailnet.ts.net scripts/mac-check.sh
set -euo pipefail
cd "$(dirname "$0")/.."
[ -f .dev.env ] && . ./.dev.env
: "${MAC_HOST:?set MAC_HOST=user@host in .dev.env}"
mac_ip=$(tailscale ip -4 "${MAC_HOST#*@}" 2>/dev/null | head -1 || true)

tar czf - --exclude target --exclude node_modules --exclude gen --exclude icons mac |
  ssh "$MAC_HOST" 'mkdir -p ~/claude-hub && cd ~/claude-hub && tar xzf -'
ssh "$MAC_HOST" 'set -e
  export PATH=$HOME/.cargo/bin:$PATH
  cd ~/claude-hub/mac
  command -v cargo-tauri >/dev/null || cargo install tauri-cli --version "^2" --locked
  [ -f src-tauri/icons/icon.icns ] || cargo tauri icon app-icon.png >/dev/null
  cargo tauri build --bundles app 2>&1 | grep -E "^(error|warning)|Finished" || true
  osascript -e "quit app \"Claude Hub\"" 2>/dev/null || true
  sleep 1
  rm -rf "/Applications/Claude Hub.app"
  cp -R "src-tauri/target/release/bundle/macos/Claude Hub.app" /Applications/
  codesign --force --deep -s - "/Applications/Claude Hub.app" 2>/dev/null
  open "/Applications/Claude Hub.app"'

if [ -n "$mac_ip" ]; then
  for _ in $(seq 1 20); do
    if ss -tn state established "( sport = :7788 )" | grep -q "$mac_ip"; then
      echo "MAC CHECK OK: app on $MAC_HOST is connected to this server"
      exit 0
    fi
    sleep 1
  done
  echo "MAC CHECK FAIL: no connection from $mac_ip within 20s"
  exit 1
fi
echo "MAC CHECK: app launched (could not resolve Tailscale IP to verify the connection)"
