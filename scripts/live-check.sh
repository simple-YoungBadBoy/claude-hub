#!/usr/bin/env bash
# Real-environment check after ./deploy/install.sh: the running service must be
# the version just built, answer on its Tailscale address, see the real tmux
# sessions, and serve the UI.
set -euo pipefail
cd "$(dirname "$0")/.."
want=$(./bin/claude-hub -version)
ip=$(tailscale ip -4 | head -1)
base="http://$ip:7788"
tok=$(cat data/token)
fail() { echo "LIVE CHECK FAIL: $*"; exit 1; }

got=$(curl -fsS -m 5 "$base/api/version" | sed -E 's/.*"version":"([^"]*)".*/\1/') || fail "service not answering on $base"
[ "$got" = "$want" ] || fail "running $got, built $want (did install.sh restart it?)"
state=$(curl -fsS -m 5 -H "Authorization: Bearer $tok" "$base/api/state") || fail "/api/state"
n_api=$(printf '%s' "$state" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["sessions"]))')
n_tmux=$(tmux ls 2>/dev/null | wc -l)
[ "$n_api" = "$n_tmux" ] || fail "hub sees $n_api sessions, tmux has $n_tmux"
curl -fsS -m 5 "$base/" | grep -q '<div id="root">' || fail "UI not served"
echo "LIVE CHECK OK: $got on $base, $n_api tmux sessions"
