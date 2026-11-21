#!/usr/bin/env bash
# Build and (re)start claude-hub as a systemd --user service.
set -euo pipefail
cd "$(dirname "$0")/.."
./build.sh
mkdir -p ~/.config/systemd/user
cp deploy/claude-hub.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now claude-hub
systemctl --user restart claude-hub
sleep 1
systemctl --user --no-pager status claude-hub | head -5
echo "token: $(cat data/token 2>/dev/null || echo '(generated on first start: data/token)')"
