#!/usr/bin/env bash
# Build the web UI (embedded into the Go binary) and the server.
set -euo pipefail
cd "$(dirname "$0")"
export GOPATH=$PWD/.gopath GOMODCACHE=$PWD/.gopath/pkg/mod GOCACHE=$PWD/.gocache GOFLAGS=-mod=mod
(cd web && npm ci --silent && npx tsc --noEmit && npx vite build --logLevel warn)
touch server/webdist/.keep   # vite empties the dir; keep the tracked placeholder
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
(cd server && go build -ldflags "-X main.Version=$VERSION" -o ../bin/claude-hub .)
echo "built bin/claude-hub $VERSION"
