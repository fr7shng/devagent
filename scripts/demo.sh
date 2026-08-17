#!/bin/sh
# devagent 一键演示（Linux/macOS）：构建并启动 daemon（mock 设备）+ sidecar
# 用法：./scripts/demo.sh
set -e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> 构建 devagent..."
go build -o "$ROOT/bin/devagent" "$ROOT/cmd/devagent/"

echo "==> 启动 daemon (mock 设备, 端口 8082)..."
"$ROOT/bin/devagent" -mode daemon -port 8082 -config "$ROOT/configs/mock_device.yaml" \
    -gateway-id gw_mock -global-config "$ROOT/configs/devagent.yaml" &
DAEMON_PID=$!
trap 'kill $DAEMON_PID 2>/dev/null' EXIT INT TERM

echo "==> 启动 sidecar (stdio MCP)。Ctrl+C 退出并清理 daemon。"
"$ROOT/bin/devagent" -mode sidecar -global-config "$ROOT/configs/devagent.yaml"
