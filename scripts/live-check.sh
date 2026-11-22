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
km=$(systemctl --user show claude-hub -p KillMode --value)
[ "$km" = process ] || fail "KillMode=$km: restarting the service would kill tmux sessions (re-run deploy/install.sh)"
main=$(systemctl --user show claude-hub -p MainPID --value)
cg=/sys/fs/cgroup$(cut -d: -f3 "/proc/$main/cgroup")
orphans=0
for p in $(cat "$cg/cgroup.procs"); do
  [ "$p" = "$main" ] && continue
  [ "$(ps -o ppid= -p "$p" | tr -d ' ')" = "$main" ] || orphans=$((orphans + 1))
done
[ "$orphans" = 0 ] || fail "$orphans process(es) left over in the service cgroup (old tmux attach clients?)"
srv=$(tmux display-message -p '#{pid}' 2>/dev/null || true)
if [ -n "$srv" ] && grep -q 'claude-hub.service' "/proc/$srv/cgroup"; then
  echo "WARN: the tmux server runs inside claude-hub.service; it will move out after the next reboot / tmux restart"
fi
curl -fsS -m 5 "$base/" | grep -q '<div id="root">' || fail "UI not served"
echo "LIVE CHECK OK: $got on $base, $n_api tmux sessions"
