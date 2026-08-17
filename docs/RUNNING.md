# 运行指南

本文带你从零把 devagent 跑起来并验证完整调用链。无需真实硬件也能验证全部核心功能（mock 设备）。

## 0. 前置要求

| 项 | 要求 |
|----|------|
| Go | 1.26+（`go version` 查看） |
| MCP 客户端（可选） | Claude Desktop / Codex / 任意 MCP 客户端 |
| 真实硬件（可选） | ESP32 + 继电器模块 |

## 1. 构建

```bash
# Windows
go build -o bin/devagent.exe ./cmd/devagent/

# Linux / macOS
make build
```

验证：`bin/devagent.exe -mode sidecar` 有输出或 `make build` 退出码 0。

## 2. 无硬件完整流程（推荐先跑这个）

需要 2 个终端。

### 终端 A：启动 daemon（模拟设备）

```bash
# Windows
.\bin\devagent.exe -mode daemon -port 8082 -config configs\mock_device.yaml -gateway-id gw_mock

# Linux / macOS
./bin/devagent -mode daemon -port 8082 -config configs/mock_device.yaml -gateway-id gw_mock
```

预期日志（JSON 到 stderr）：

```
"msg":"注册设备工具","tool":"mock_gw.set_relay"
"msg":"注册设备工具","tool":"mock_gw.read_status"
"msg":"mDNS 广播已启动","gateway_id":"gw_mock","port":8082"
```

### 终端 B：验证 HTTP 链路

先准备 invoke 请求体（PowerShell 下用文件传 JSON 避免引号问题）：

```powershell
Set-Content -Path invoke.json -Value '{"type":"invoke","request_id":"r1","device_id":"mock_gw","capability":"set_relay","params":{"pin":1,"state":true}}'
```

```bash
# 1. 设备列表 —— 应返回 mock_gw 的能力模型
curl http://localhost:8082/devices

# 2. 调用能力 set_relay(pin=1, state=true)
curl -X POST http://localhost:8082/invoke -H "Content-Type: application/json" --data-binary @invoke.json

# 3. 健康检查
curl http://localhost:8082/healthz
```

预期 `set_relay` 返回：

```json
{"type":"invoke_result","device_id":"mock_gw","capability":"set_relay","status":"ok",
 "result":{"status":"ok","stdout":"mock relay pin=1 state=1\r\n","exit_code":0}}
```

### 终端 C：启动 sidecar（AI 入口）

```bash
.\bin\devagent.exe -mode sidecar
```

- 若 mDNS 正常：日志出现 `"msg":"发现网关","gateway_id":"gw_mock"` 和 `注册设备工具 mock_gw.set_relay`。
- **sidecar 与 daemon 同机时 mDNS 多播不会回环，发现不到是正常的**。改用静态网关（推荐，见下）。

### 同机运行的推荐方式：静态网关

在 `configs/devagent.yaml` 配置 `static_gateways` 指向 daemon，sidecar 启动时直接注册，不依赖 mDNS：

```yaml
sidecar:
  static_gateways:
    - { id: "gw_mock", url: "http://localhost:8082" }
```

重启 sidecar 后，日志应出现 `注册静态网关 gw_mock` 和 `注册设备工具 mock_gw.set_relay`。

## 3. 接入 AI（可选）

把 sidecar 配为 MCP server：

```json
{
  "mcpServers": {
    "devagent": { "command": "devagent", "args": ["-mode", "sidecar"] }
  }
}
```

对 AI 说：*"打开 mock_gw 的继电器 1"*。AI 会调用 `mock_gw.set_relay`（参数 pin=1, state=true），返回上面那条 `invoke_result`。

内置系统工具：`__system__.list_devices`、`__system__.get_device_schema`、`__system__.get_job_status`、`__system__.diagnose_connectivity`。

## 4. 一键体验脚本

不想手动开终端：

```bash
# Windows
.\scripts\demo.ps1

# Linux / macOS
./scripts/demo.sh
```

自动构建 → 起 mock daemon → 起 sidecar。Ctrl+C 退出并清理。

## 5. CLI 辅助命令

```bash
devagent init                          # 生成 devagent.yaml + device.yaml 模板
devagent validate configs/mock_device.yaml   # 校验物模型
devagent schema configs/mock_device.yaml     # 打印能力摘要（含 intent_id）
```

## 6. 真实硬件（ESP32）

1. 烧录固件（需 [ESP-IDF v5.x](https://docs.espressif.com/projects/esp-idf/)）：
   ```bash
   cd internal/lite
   idf.py set-target esp32
   idf.py build
   idf.py -p /dev/ttyUSB0 flash        # Windows: -p COM3
   ```
2. daemon 改用串口物模型：
   ```bash
   devagent -mode daemon -port 8081 -config configs/example_device.yaml -gateway-id gw_01
   ```
   > 物模型里 `channel` 对应你的串口：Windows 用 `COM3`，Linux 用 `/dev/ttyUSB0`。
   > `go.bug.st/serial` 依赖 CGo；`CGO_ENABLED=0` 构建时串口不可用。

## 7. 常见问题

| 现象 | 原因 / 处理 |
|------|-------------|
| sidecar 不发现 daemon | **同机运行时 mDNS 多播不回环，属正常**；配置 `static_gateways` 指向 daemon 即可 |
| 串口打不开 / HAL 不可用 | 检查物模型 `channel`/`baudrate`；CGo 构建要求 |
| 端口被占 | 换 `-port`，或 `netstat -ano \| findstr :8082` 查占用进程 |
| 设备被"心跳超时移除" | 已修复：静态配置设备不会被心跳移除（v0.4.0 起） |
| Ctrl+C 后仍有进程残留 | `Get-Process devagent \| Stop-Process`（Windows） |

## 8. 验证清单

- [ ] `go build` 成功
- [ ] daemon 日志出现"注册设备工具"
- [ ] `curl /devices` 返回设备模型
- [ ] `curl /invoke set_relay` 返回 `status=ok` 且 stdout 含 `pin=1 state=1`
- [ ] （可选）sidecar 日志出现"发现网关"
- [ ] （可选）AI 能调用 `mock_gw.set_relay`
