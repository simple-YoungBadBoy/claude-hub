#!/usr/bin/env bash
# The gate every change must pass before it is pushed (also run by the
# pre-push hook and CI): format, vet, unit tests, type check, build, e2e.
set -euo pipefail
cd "$(dirname "$0")/.."

# Small dev servers (2 CPU / 4 GB running other services) were frozen by an
# unbounded run (2026-11-21). Outside CI, re-run inside a memory-capped,
# low-priority cgroup: if the budget is exceeded only the check dies.
if [ -z "${CHECK_LIMITED:-}" ] && [ -z "${CI:-}" ] && command -v systemd-run >/dev/null &&
  systemd-run --user --scope -q -p MemoryMax=64M true 2>/dev/null; then
  export CHECK_LIMITED=1
  exec systemd-run --user --scope -q -p MemoryMax="${CHECK_MEM_MAX:-1400M}" -p MemorySwapMax=512M \
    nice -n 10 "$0" "$@"
fi
export GOPATH=$PWD/.gopath GOMODCACHE=$PWD/.gopath/pkg/mod GOCACHE=$PWD/.gocache GOFLAGS=-mod=mod

step() { printf '\n\033[1;33m==> %s\033[0m\n' "$*"; }

# go:embed needs server/webdist to exist even before the web build (fresh clone)
mkdir -p server/webdist && touch server/webdist/.keep

step "go: gofmt / vet / test -race"
(
  cd server
  bad=$(gofmt -l .)
  [ -z "$bad" ] || { echo "gofmt needed: $bad"; exit 1; }
  go vet ./...
  go test -race -count=1 -p 1 ./...   # one package at a time: -race is memory hungry
)

step "build (web type check + bundle, server binary)"
./build.sh

step "e2e (real server, private tmux, headless Chromium)"
(cd e2e && npm ci --silent && node e2e.mjs)

if [ "$(uname)" = Darwin ] && command -v cargo >/dev/null; then
  step "mac: cargo check"
  (cd mac/src-tauri && cargo check --quiet)
fi

printf '\n\033[1;32mALL CHECKS PASSED\033[0m\n'
