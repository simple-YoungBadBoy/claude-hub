#!/usr/bin/env bash
# Build and (re)start claude-hub as a systemd --user service.
set -euo pipefail
cd "$(dirname "$0")/.."
./build.sh
mkdir -p ~/.config/systemd/user
cp deploy/claude-hub.service ~/.config/systemd/user/
systemctl --user daemon-reload
before=$(tmux ls -F '#{session_name}' 2>/dev/null | sort || true)
systemctl --user enable --now claude-hub
systemctl --user restart claude-hub
sleep 1
after=$(tmux ls -F '#{session_name}' 2>/dev/null | sort || true)
if [ "$before" != "$after" ]; then
  echo "ERROR: tmux sessions changed across the restart"; echo "before: $before"; echo "after:  $after"; exit 1
fi
systemctl --user --no-pager status claude-hub | head -5
echo "token: $(cat data/token 2>/dev/null || echo '(generated on first start: data/token)')"
