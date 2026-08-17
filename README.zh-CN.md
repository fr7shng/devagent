# devagent

> **[English](README.md)** · 中文

> **让任何设备接入你的 AI。** 一个轻量级 Go 桥接程序，让 LLM 通过 [MCP](https://modelcontextprotocol.io) 协议直接发现和控制局域网设备——裸 MCU、PC、OpenWrt 路由器。

[![CI](https://github.com/fr7shng/devagent/actions/workflows/ci.yml/badge.svg)](https://github.com/fr7shng/devagent/actions)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26-blue)](go.mod)

<p align="center">
  <img src="docs/demo.gif" alt="AI controlling an ESP32 relay via devagent" width="640"/>
</p>

让 AI 翻转 ESP32 上的继电器、读取传感器、或在路由器上执行命令——devagent 把请求转成类型化的 MCP 工具调用，通过 mDNS 发现正确的设备，并用与其 Flash 大小匹配的协议驱动它。

## 为什么选择 devagent

大多数"AI + 硬件"方案都依赖云平台。devagent 恰恰相反：**单一静态二进制**、**零配置**（mDNS 自动发现）、**本地优先**。它支持两种串口协议，所以既能触达 8KB Flash 的 STM32，也能触达完整的 OpenWrt 路由器。

| | devagent | Home Assistant + MCP | Raw MQTT + custom MCP |
|---|---|---|---|
| 目标 | 裸 MCU、DIY 硬件、路由器 | 智能家居生态 | 任何用 MQTT 的设备 |
| 部署 | 一个二进制，无云 | 完整服务栈 | 需要 broker + 适配器 |
| 设备发现 | 自动（mDNS） | 手动配对 | 手动 topic |
| MCU 开销 | ~2.7 KB SRAM / <5 KB flash | n/a | n/a |

## 亮点

- **动态 MCP 工具** — 设备上线/下线时自动注册/注销工具（`{device}.{capability}`）；维护状态也会对 AI 呈现。
- **双串口协议** — `uRPC`（6 字节帧、CRC8、面向极小 MCU）和 `DCP`（CBOR + HMAC、面向 ESP32 级设备）。
- **零配置发现** — daemon 通过 mDNS 广播；sidecar 自动找到它们和它们的设备。
- **能力级安全** — HMAC 签名的能力 token，粒度到单设备单能力，或使用 `*`。
- **异步设备调用** — 工具立即返回 `job_id`，用 `__system__.get_job_status` 轮询。
- **跨平台** — Windows、macOS、Linux、OpenWrt (mipsle)，外加 `internal/lite` 下的 ESP-IDF 固件。

## 架构

```
AI (Claude / Codex)
    │ stdio MCP
    ▼
┌──────────┐  HTTP/SSE  ┌──────────┐   uRPC or     ┌──────────┐
│ Sidecar  │◄──────────►│  Daemon  │◄─────────────►│  ESP32   │
│ (stdio)  │  /invoke   │  (SSE)   │   DCP/CBOR    │  (Lite)  │
└──────────┘            └──────────┘                └──────────┘
   route table            device registry            <3 KB SRAM
   dedup / progress       state snapshot / rate limit
   dynamic tool register  HAL dispatch
   HMAC token verify      SerialBridge / DCPBridge
```

- **Sidecar** — AI 的唯一入口。stdio MCP server、mDNS 发现、动态工具注册、去重 + 异步任务跟踪。
- **Daemon** — 运行在硬件旁边（PC、路由器、网关）。加载 YAML 物模型，注册设备的 MCP 工具，并把调用分发给对应的 bridge。
- **Lite** — ESP-IDF 固件（`internal/lite`），实现带 pin/state 范围校验的 uRPC agent。

## 快速开始（5 分钟）

### 1. 构建

```bash
# 需要 Go 1.26+
make build              # 或：go build -o bin/devagent ./cmd/devagent/
```

### 2. 用物模型运行 daemon

```bash
./bin/devagent -mode daemon -port 8081 -config configs/example_device.yaml -gateway-id gw_01
```

`configs/example_device.yaml` 描述一个通过串口代理的继电器 + 温度设备。它会以 `_devagent._tcp` 广播 mDNS。

**没有硬件？** 使用 mock 设备——整个链路不需要串口：

```bash
./bin/devagent -mode daemon -port 8082 -config configs/mock_device.yaml -gateway-id gw_mock
```

或试试一键 demo：`./scripts/demo.sh`（Linux/macOS）或 `.\scripts\demo.ps1`（Windows）。

### 3. 运行 sidecar

```bash
./bin/devagent -mode sidecar
```

sidecar 发现 daemon、拉取 `/devices`，并注册 `shelf_01.set_relay` 等工具。

> **同一台机器？** mDNS 多播不会回环，所以 sidecar 与 daemon 同主机时自动发现无效。改用 `static_gateways` 把 sidecar 指向 daemon（见[配置](#配置)）。

### 4. 接入你的 AI

把 MCP 客户端指向 sidecar（stdio）：

```json
{
  "mcpServers": {
    "devagent": { "command": "devagent", "args": ["-mode", "sidecar"] }
  }
}
```

然后问：*"打开 shelf_01 的继电器 1"*。AI 会看到 `shelf_01.set_relay`，调用链路为 Sidecar → HTTP → Daemon → SerialBridge → ESP32 ACK。

### 内置工具

| 工具 | 用途 |
|------|------|
| `__system__.list_devices` | 列出已注册设备及状态 |
| `__system__.diagnose_connectivity` | 检查到指定设备的完整链路 |
| `__system__.get_device_schema` | 获取设备的能力 schema |
| `__system__.get_job_status` | 按 `job_id` 轮询异步任务 |

## 物模型（YAML）

能力用 YAML 声明并编译为带输入校验（enum/min/max/unit）的 MCP 工具。`implementation.protocol` 选择 `uRPC` 或 `DCP`；`native: "shell"` 改为执行本地命令。

```yaml
device:
  id: "shelf_01"
  name: "Shelf controller 01"
  type: "mcu_proxy"

capabilities:
  - name: set_relay
    description: "control a relay"
    inputSchema:
      type: object
      properties:
        pin:   { type: integer, enum: [1, 2, 3], unit: "gpio_pin" }
        state: { type: boolean, unit: "on_off" }
      required: [pin, state]
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"     # Linux；Windows 用 COM3
      baudrate: 115200
      protocol: "uRPC"            # 或 "DCP"
      cmd_map:
        set_relay: { cmd: 161, fmt: "{pin} {state}" }
      timeout_ms: 5000
      retry: 3
```

完整参考见 [docs/device-model.md](docs/device-model.md)。

## 双协议

| | uRPC | DCP (CBOR) |
|---|---|---|
| 帧开销 | 6 字节 + | 39 字节 + |
| MCU RAM | ~2.7 KB | 0.6 KB |
| MCU Flash | <5 KB | 27.6 KB |
| 完整性 | CRC8 | HMAC-SHA256 |
| 返回值 | 无（仅命令型 ACK） | 有（CBOR payload） |
| 适用 | <8 KB Flash 的 MCU | ESP32 级设备 |

## 安全

在 `configs/devagent.yaml` 中设置 `token.secret`（或 `DEVAGENT_TOKEN_SECRET`）。sidecar 与 daemon 共享该密钥；每次 sidecar 请求会 mint 一个短时能力 token，daemon 按请求的能力校验：

```go
tm := sidecar.NewTokenManager("my-secret")
token, _ := tm.Mint([]string{"shelf_01.set_relay"}, time.Hour)
claims, _ := tm.Verify(token)
tm.CheckCap(claims, "shelf_01.set_relay")  // true
```

能力以 `{device}.{capability}` 命名；`*` 授予全部。daemon HTTP 服务支持 TLS（`DEVAGENT_TLS_CERT` / `DEVAGENT_TLS_KEY`）。

## 配置

全局配置在 `configs/devagent.yaml`（可用 `DEVAGENT_TOKEN_SECRET` 等环境变量覆盖）：

```yaml
sidecar:
  mdns_interval: 10s
  dedup_ttl: 3s
  health_check_interval: 30s
  maintenance_timeout: 60s
  heartbeat_timeout: 90s
  static_gateways:            # 可选：绕过 mDNS（同机 / 无 mDNS 环境）
    # - { id: "gw_mock", url: "http://localhost:8082" }

daemon:
  heartbeat_interval: 30s
  heartbeat_timeout: 60s
  state_path: ""

log_level: info

token:
  secret: ""                  # 留空则关闭 token 校验
  default_ttl: 3600s
```

包含烧录真实硬件的分步演练见 [docs/RUNNING.md](docs/RUNNING.md)。

## 平台矩阵

| 平台 | 架构 | 串口 | mDNS | 二进制 |
|----------|------|--------|------|--------|
| Windows | amd64, arm64 | ✅ | ✅ 原生 | `devagent-windows-amd64.exe` |
| Linux | amd64, arm64 | ✅ | ⚠️ avahi | `devagent-linux-amd64` |
| macOS | amd64, arm64 | ✅ | ✅ Bonjour | `devagent-darwin-arm64` |
| OpenWrt | mipsle | ⚠️ CGO 工具链 | ⚠️ umdns | `devagent-openwrt-mipsle`（~5 MB UPX） |

> `go.bug.st/serial` 依赖 CGo；`CGO_ENABLED=0` 时串口不可用（影响 OpenWrt 构建，见 [docs/openwrt.md](docs/openwrt.md)）。

## 项目结构

```
cmd/devagent/            # CLI（sidecar / daemon 模式，init/validate/schema）
cmd/integration_test/    # 集成测试（go run，非 go test）
internal/model/          # 数据模型 + 并发路由表
internal/mcptool/        # 能力 → MCP 工具编译器（sidecar/daemon 共用）
internal/sidecar/        # MCP server、mDNS router、去重、异步任务
internal/daemon/         # 设备注册表、HAL bridge、native handler
internal/protocol/       # uRPC、DCP/CBOR、SSE 消息编解码
internal/auth/           # HMAC 能力 token
internal/version/        # 版本常量
internal/lite/           # ESP32 uRPC 固件（ESP-IDF 工程）
configs/                 # 全局配置 + 物模型（含 mock）
scripts/                 # demo.ps1 / demo.sh / mock_relay.sh
docs/                    # 运行、物模型、安全、OpenWrt、DCP 指南
deploy/                  # systemd unit + logrotate
```

## 开发

```bash
make build        # 构建当前平台二进制
make test         # 单元测试
make vet          # go vet
make integration  # 集成测试（go run ./cmd/integration_test/）
make cross        # 交叉编译矩阵（Windows/Linux/macOS/OpenWrt）
```

### CLI 子命令

```bash
devagent init                          # 生成 devagent.yaml + device.yaml 模板
devagent validate <device.yaml>        # 校验物模型
devagent schema <device.yaml>          # 打印能力摘要（含 intent_id）
```

### Docker

daemon 可容器化运行（mock/native 能力；串口需要 CGo 镜像）：

```bash
docker compose up daemon
```

## 常见问题

**没有 mDNS 能用吗？** 可以。在 `configs/devagent.yaml` 中设置 `static_gateways` 直接指向 daemon——sidecar 与 daemon **同机**（mDNS 多播不回环）或 mDNS 被屏蔽时必须这样：

```yaml
sidecar:
  static_gateways:
    - { id: "gw_mock", url: "http://localhost:8082" }
```

**支持哪些 MCU？** 任何实现约 100 行 uRPC agent（见 `internal/lite`）的 MCU。DCP 路径面向 ESP32 级设备，使用 [DCP 固件](https://github.com/device-context-protocol/dcp)。

**为什么 `read_temp` 只返回确认？** 5 字节的 uRPC ACK 没有 payload 通道——uRPC 是面向极小 MCU 的纯命令型协议。需要返回值时改用 DCP 路径。

**为什么有两个 MCP 入口？** sidecar 是标准路径：发现 daemon、异步分发、返回可轮询的 `job_id`。daemon 也暴露一个同步 MCP server（SSE），用于单机直连调试——它会在调用内等待串口 ACK，不使用任务表。除非在同主机调试单个设备，否则使用 sidecar。

## 许可证

[Apache-2.0](LICENSE)。`internal/lite` 下的 ESP32 固件同样为 Apache-2.0。
