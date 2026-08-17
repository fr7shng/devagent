#!/bin/sh
# devagent one-command demo (Linux/macOS): build, start a mock daemon, then the sidecar.
# Usage: ./scripts/demo.sh
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> Building devagent..."
go build -o "$ROOT/bin/devagent" "$ROOT/cmd/devagent/"

echo "==> Starting daemon (mock device, port 8082)..."
"$ROOT/bin/devagent" -mode daemon -port 8082 -config "$ROOT/configs/mock_device.yaml" \
    -gateway-id gw_mock -global-config "$ROOT/configs/devagent.yaml" &
DAEMON_PID=$!
trap 'kill $DAEMON_PID 2>/dev/null' EXIT INT TERM

echo "==> Starting sidecar (stdio MCP). Press Ctrl+C to exit and stop the daemon."
"$ROOT/bin/devagent" -mode sidecar -global-config "$ROOT/configs/devagent.yaml"
