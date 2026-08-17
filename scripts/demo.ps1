# devagent 一键演示（Windows）：构建并启动 daemon（mock 设备）+ sidecar
# 用法：.\scripts\demo.ps1
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Write-Host "==> 构建 devagent..." -ForegroundColor Cyan
Push-Location $root
go build -o bin/devagent.exe ./cmd/devagent/
if ($LASTEXITCODE -ne 0) { Pop-Location; exit 1 }
Pop-Location

Write-Host "==> 启动 daemon (mock 设备, 端口 8082)..." -ForegroundColor Cyan
$daemon = Start-Process -FilePath "$root\bin\devagent.exe" `
    -ArgumentList "-mode daemon -port 8082 -config configs/mock_device.yaml -gateway-id gw_mock -global-config configs/devagent.yaml" `
    -PassThru -WindowStyle Hidden

Write-Host "==> 启动 sidecar (stdio MCP)。Ctrl+C 退出并清理 daemon。" -ForegroundColor Cyan
try {
    & "$root\bin\devagent.exe" -mode sidecar -global-config configs/devagent.yaml
} finally {
    if ($daemon -and -not $daemon.HasExited) { Stop-Process -Id $daemon.Id -Force }
}
