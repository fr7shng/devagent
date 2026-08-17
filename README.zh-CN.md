# devagent — Go DCP Bridge

> **[English](README.md)** · 中文

轻量设备守护程序，AI 通过 MCP 协议直接发现和控制局域网设备。兼容 [DCP (Device Context Protocol)](https://github.com/device-context-protocol/dcp)，专注"部署简单（单二进制）、多设备路由、双协议支持"。

## 与 DCP 的关系

devagent 是 DCP 的 Go Bridge 实现：

| | DCP (Python) | devagent (Go) |
|---|---|---|
| 定位 | 协议规范 + Python Bridge | Go Bridge + 独立部署 |
| 部署 | `pip install pydcp` | 单二进制 `go build` |
| 多设备 | 单设备单 Bridge | 一个 Sidecar 管理多 Daemon |
| 动态 Tool | 静态 manifest | 设备上线/下线自动注册/注销 |
| MCU 协议 | CBOR only | CBOR (DCP) + uRPC 双协议 |
| 安全 | HMAC token | HMAC token + 去重 |

## 架构

```
AI (Claude Code / Codex)
    │ stdio MCP
    ▼
┌──────────┐  HTTP/SSE  ┌──────────┐   DCP/CBOR   ┌──────────┐
│ Sidecar  │◄──────────►│  Daemon  │◄─────────────►│  ESP32   │
│ (stdio)  │  /invoke   │  (SSE)   │      或       │  (Lite)  │
└──────────┘            └──────────┘   uRPC/串口   └──────────┘
  路由表/mDNS            设备注册表                 <3KB SRAM
  去重/进度              状态快照/限流
  动态Tool注册           HAL双协议调度
  HMAC Token校验         DCPBridge/SerialBridge
```

- **Sidecar**：AI 唯一入口，stdio MCP Server，自动发现 Daemon，动态注册/注销设备 Tool，HMAC token 校验
- **Daemon**：代理 MCU/PC/OpenWrt，加载物模型后注册设备 MCP Tool，根据协议自动选择 DCP Bridge 或 SerialBridge 执行
- **Lite**：ESP32 固件，uRPC 协议（<3KB SRAM）或 DCP CBOR 协议（0.6KB RAM），含 pin 范围校验

## 平台支持

devagent 基于 Go 编译，天然跨平台。以下是各平台的部署状态和注意事项：

### 桌面/服务器

| 平台 | 架构 | 交叉编译 | 串口支持 | mDNS | 部署方式 |
|------|------|---------|---------|------|---------|
| **Windows** | amd64, arm64 | ✅ 已验证 | ✅ | ✅ 原生 | `go build` → `devagent.exe` |
| **Linux (Ubuntu/Debian)** | amd64, arm64 | ✅ | ✅ | ⚠️ 需 avahi | `go build` → `devagent` |
| **macOS** | arm64, amd64 | ✅ | ✅ | ✅ Bonjour | `go build` → `devagent` |

编译命令：

```bash
# Windows
GOOS=windows GOARCH=amd64 go build -o bin/devagent-windows-amd64.exe ./cmd/devagent/

# Linux
GOOS=linux GOARCH=amd64 go build -o bin/devagent-linux-amd64 ./cmd/devagent/

# Linux ARM (树莓派等)
GOOS=linux GOARCH=arm64 go build -o bin/devagent-linux-arm64 ./cmd/devagent/

# macOS
GOOS=darwin GOARCH=arm64 go build -o bin/devagent-darwin-arm64 ./cmd/devagent/
```

### 嵌入式 Linux (OpenWrt/嵌入式)

| 平台 | 架构 | 交叉编译 | 串口支持 | mDNS | 二进制大小 | 注意事项 |
|------|------|---------|---------|------|-----------|---------|
| **OpenWrt** | mipsle | ✅ | ⚠️ 需交叉工具链 | ⚠️ 需 umdns | ~5MB (upx后) | 资源受限 |
| **OpenWrt** | arm | ✅ | ⚠️ 需交叉工具链 | ⚠️ 需 umdns | ~4MB (upx后) | 较好 |
| **嵌入式 Linux** | arm64 | ✅ | ✅ | ⚠️ 需 avahi | ~8MB | 推荐 |

OpenWrt 编译：

```bash
# 安装交叉工具链 (mipsle 示例)
# OpenWrt SDK 中获取 mipsel-openwrt-linux-gcc

# 方式1：纯 Go 编译 (不需要 CGo)
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat \
  go build -ldflags="-s -w" -o bin/devagent-openwrt-mipsle ./cmd/devagent/

# 方式2：使用 upx 压缩 (从 ~12MB 压到 ~5MB)
upx --best bin/devagent-openwrt-mipsle

# 上传到路由器
scp bin/devagent-openwrt-mipsle root@192.168.1.1:/usr/bin/devagent
```

OpenWrt 注意事项：
- 串口库 `go.bug.st/serial` 默认用 CGo，`CGO_ENABLED=0` 编译时串口功能不可用
- mDNS 需要 `opkg install umdns` 或用固定配置替代
- 路由器 Flash 空间有限，建议 upx 压缩
- 适合作为 **Daemon 网关**运行，代理下游 ESP32/MCU 设备

### MCU (微控制器)

| 平台 | 协议 | RAM | Flash | 编译方式 |
|------|------|-----|-------|---------|
| **ESP32** | uRPC | ~2.7KB | <5KB | ESP-IDF v5.x |
| **ESP32** | DCP/CBOR | 0.6KB | 27.6KB | Arduino/ESP-IDF |
| **STM32** | uRPC | ~2KB | <5KB | Makefile + arm-none-eabi-gcc |

MCU 不运行 devagent Go 程序，而是运行 `internal/lite/urpc_agent.c` 或 DCP 固件，由 Daemon 通过串口控制。

### 典型部署方案

```
方案1：PC + MCU
┌─────────────┐     ┌─────────────┐
│ Sidecar+Daemon │────串口────│   ESP32    │
│   (笔记本)    │             │  (uRPC)    │
└─────────────┘     └─────────────┘

方案2：PC + 路由器 + MCU (推荐)
┌─────────┐  SSE  ┌───────────┐  串口  ┌─────────┐
│ Sidecar │◄─────►│  Daemon   │───────►│  ESP32  │
│  (PC)   │       │(OpenWrt)  │        │ (uRPC)  │
└─────────┘      └───────────┘        └─────────┘
                    │
                    ├─ 控制路由器自身 (iptables/wifi)
                    └─ 代理更多串口设备

方案3：纯 PC 集群
┌─────────┐  SSE  ┌───────────┐
│ Sidecar │◄─────►│  Daemon   │→ shell_exec (PC-1)
│  (PC)   │       │  (PC-1)   │
│         │  SSE  ├───────────┤
│         │◄─────►│  Daemon   │→ shell_exec (PC-2)
│         │       │  (PC-2)   │
└─────────┘      └───────────┘
```

## 快速开始

### 编译

```bash
# 当前平台
make build

# 交叉编译 Linux
GOOS=linux GOARCH=amd64 go build -o bin/devagent-linux ./cmd/devagent/

# 交叉编译 OpenWrt (mipsle, 无 CGo)
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat \
  go build -ldflags="-s -w" -o bin/devagent-openwrt ./cmd/devagent/
```

### 测试

```bash
make test          # 单元测试 (80 tests)
go run ./cmd/integration_test/   # 集成测试 (32 checks)
```

### 无硬件快速体验（推荐）

不依赖任何硬件即可跑通完整调用链——mock 设备用本地命令模拟继电器：

```bash
# 一键演示：构建 + 起 mock daemon + 起 sidecar
.\scripts\demo.ps1        # Windows
./scripts/demo.sh         # Linux/macOS

# 或手动：起 mock daemon
devagent -mode daemon -port 8082 -config configs/mock_device.yaml -gateway-id gw_mock
```

> **同机运行注意**：sidecar 与 daemon 在同一台机器时，mDNS 多播不会回环，自动发现会失败。请在 `configs/devagent.yaml` 配置 `static_gateways` 指向 daemon（见"全局配置"）。

### Claude Desktop 配置

启动时日志会输出 Claude 配置，直接复制到 `claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "devagent": {
      "command": "devagent",
      "args": ["-mode", "sidecar"]
    }
  }
}
```

## 使用方式

### Sidecar 模式（AI 侧）

```bash
# Windows
devagent.exe -mode sidecar

# Linux/macOS
./devagent -mode sidecar
```

自动通过 mDNS 发现局域网内的 Daemon，收到设备注册后动态添加 MCP Tool。

### Daemon 模式（设备侧）

```bash
# Windows (PC 直连)
devagent.exe -mode daemon -port 8081 -config configs/pc_device.yaml -gateway-id gw_pc

# Linux (PC/服务器)
./devagent -mode daemon -port 8081 -config configs/pc_device.yaml -gateway-id gw_pc

# Linux (MCU 网关)
./devagent -mode daemon -port 8081 -config configs/example_device.yaml -gateway-id gw_01

# OpenWrt (路由器网关)
./devagent -mode daemon -port 8081 -config /etc/devagent/device.yaml -gateway-id gw_router

# DCP 设备（protocol: "DCP"，示例见 docs/dcp-firmware.md）
./devagent -mode daemon -port 8081 -config dcp_device.yaml -gateway-id gw_dcp
```

Daemon 加载 YAML 物模型，根据 `implementation.protocol` 自动选择 DCP Bridge（CBOR）或 SerialBridge（uRPC）。

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-mode` | `sidecar` | 运行模式：`sidecar` 或 `daemon` |
| `-port` | `8080` | Daemon 监听端口 |
| `-config` | 空 | 设备物模型 YAML 配置路径（可用相对或绝对路径） |
| `-gateway-id` | `gw_1` | 网关 ID |
| `-log-level` | `info` | 日志级别：`debug` / `info` / `warn` / `error` |

## 核心调用链路

### uRPC 路径（极小 MCU）

```
AI → Sidecar → HTTP POST /invoke → Daemon
    → SerialBridge.SendURPC → 串口 → ESP32 ACK → 返回结果
    （串口读超时/ACK 校验失败时在 Bridge 内重试，最多 3 次）
```

### DCP 路径（兼容 DCP 固件）

```
AI → Sidecar → HTTP POST /invoke → Daemon
    → DCPBridge.SendDCP → CBOR帧 → 串口 → DCP Reply → 返回结果
    （读取失败时在 Bridge 内重试，最多 3 次）
```

## MCP 工具列表

### 系统工具（Sidecar 内置）

| 工具名 | 说明 | 参数 |
|--------|------|------|
| `__system__.list_devices` | 列出所有已注册设备及状态 | 无 |
| `__system__.diagnose_connectivity` | 检查到指定设备的全链路状态 | `device_id` (必填) |
| `__system__.get_device_schema` | 获取指定设备的完整能力描述 | `device_id` (必填) |
| `__system__.get_job_status` | 查询异步任务状态 | `job_id` (必填) |

### 设备工具（动态注册）

设备上线时自动注册，格式：`{device_id}.{capability}`，设备下线时自动注销。

## 物模型配置

### MCU 代理设备（uRPC）

```yaml
device:
  id: "shelf_01"
  name: "货架控制器01"
  type: "mcu_proxy"

capabilities:
  - name: set_relay
    description: "控制继电器通断"
    intent_id: 4660              # DCP intent_id（可选，不填则自动计算）
    inputSchema:
      type: object
      properties:
        pin: { type: integer, enum: [1, 2, 3], unit: "gpio_pin" }
        state: { type: boolean, unit: "on_off" }
      required: [pin, state]
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"    # Linux: /dev/ttyUSB0, Windows: COM3
      baudrate: 115200
      protocol: "uRPC"           # 或 "DCP"
      cmd_map:
        set_relay: { cmd: 161, fmt: "{pin} {state}" }
      timeout_ms: 5000
      retry: 3
```

### PC 直连设备

```yaml
device:
  id: "my_pc"
  name: "我的电脑"
  type: "direct"

capabilities:
  - name: shell_exec
    description: "执行shell命令"
    inputSchema:
      type: object
      properties:
        command: { type: string, unit: "shell_command" }
      required: [command]
    implementation:
      native: "shell"
      timeout_ms: 30000
```

### OpenWrt 路由器设备

```yaml
device:
  id: "gw_router"
  name: "主路由器"
  type: "direct"

capabilities:
  - name: set_firewall
    description: "设置防火墙规则"
    inputSchema:
      type: object
      properties:
        rule: { type: string, unit: "iptables_rule" }
      required: [rule]
    implementation:
      native: "shell"
      timeout_ms: 10000

  - name: get_traffic
    description: "获取流量统计"
    inputSchema:
      type: object
      properties: {}
    implementation:
      native: "shell"
      timeout_ms: 5000

  - name: control_relay
    description: "控制GPIO继电器"
    inputSchema:
      type: object
      properties:
        pin: { type: integer, unit: "gpio_pin" }
        state: { type: boolean, unit: "on_off" }
      required: [pin, state]
    implementation:
      proxy: "uart"
      channel: "/dev/ttyS1"      # OpenWrt 串口
      baudrate: 115200
      protocol: "uRPC"
      cmd_map:
        set_relay: { cmd: 161, fmt: "{pin} {state}" }
      timeout_ms: 5000
      retry: 3
```

### inputSchema 属性

| 类型 | 可选约束 |
|------|----------|
| `integer` | `enum`, `min`/`max`, `unit` |
| `string` | `unit` |
| `boolean` | `unit` |
| `number` | `min`/`max`, `unit` |

`unit` 追加到工具描述中（如 `pin(gpio_pin), state(on_off)`）。`intent_id` 为 DCP 兼容字段，不填则通过 `SHA256(name)` 取前 2 字节自动计算。

## 双协议支持

| | uRPC | DCP (CBOR) |
|---|---|---|
| 帧开销 | 6字节+ | 39字节+ |
| MCU RAM | ~2.7KB | 0.6KB |
| MCU Flash | <5KB | 27.6KB |
| 安全 | CRC8 校验 | HMAC-SHA256 签名 |
| 适用 | 极小 MCU (<8KB flash) | ESP32 等 28KB+ flash |

YAML 中 `implementation.protocol` 设为 `"uRPC"` 或 `"DCP"` 即可切换，Daemon 自动选择对应的 Bridge。

## HMAC Capability Token

参照 DCP 安全模型实现。配置 `devagent.yaml` 中的 `token.secret` 后生效：

```go
tm := sidecar.NewTokenManager("my-secret")
token, _ := tm.Mint([]string{"shelf_01.set_relay", "shelf_01.read_temp"}, time.Hour)
claims, _ := tm.Verify(token)
tm.CheckCap(claims, "shelf_01.set_relay")  // true
tm.CheckCap(claims, "motor.start")         // false
```

支持通配符 `"*"` 授权所有能力。

## 全局配置

`configs/devagent.yaml`：

```yaml
sidecar:
  mdns_interval: 10s
  dedup_ttl: 3s
  health_check_interval: 30s
  maintenance_timeout: 60s
  heartbeat_timeout: 90s
  # 静态网关（可选）：sidecar 与 daemon 同机运行、或 mDNS 不可用时直接指定，
  # 绕过 mDNS 自动发现（同机运行时 mDNS 多播不会回环）
  # static_gateways:
  #   - { id: "gw_mock", url: "http://localhost:8082" }

daemon:
  heartbeat_interval: 30s
  heartbeat_timeout: 60s
  state_path: ""

log_level: info

token:
  secret: ""          # 留空则不校验 token
  default_ttl: 3600   # token 有效期（秒）
```

## uRPC 协议

用于极小 MCU 的二进制串口协议：

```
请求: [0xAA] [Seq] [Cmd] [Len] [Payload:N] [CRC8]
ACK:  [0xBB] [Seq] [Status] [QueueDepth] [CRC8]
```

状态码：0x00 OK / 0x01 BUSY / 0x02 INVALID_CMD / 0xFF ERROR

## DCP 协议

兼容 [DCP v0.3](https://github.com/device-context-protocol/dcp) 的 CBOR 帧格式：

```
[Ver:1] [Kind:1] [Seq:1] [IntentID:2] [Len:1] [CBOR Payload:N] [HMAC:16]
```

Kind：0x01 Call / 0x02 Reply / 0x03 Event / 0x04 Error / 0x81 DryRun

IntentID = SHA256(intent name) 前 2 字节，与 DCP 固件保持同步。

## 重试与超时

串口调用的超时（默认 5s）与重试（最多 3 次）由 `DCPBridge`/`SerialBridge` 内部管理，调用方只需处理最终错误；串口断开时由 Daemon 后台定时重连。

## HAL 硬件抽象层

```go
type HAL interface {
    Open(channel string, baudrate int) error
    SendURPC(ctx, *URPCRequest) (*URPCAck, error)     // uRPC 路径
    SendDCP(ctx, seq, intentID, params) (*DCPFrame, error)  // DCP 路径
    Close() error
    Transport() TransportType  // "urpc" 或 "dcp"
}
```

- `SerialBridge`：uRPC 串口实现
- `DCPBridge`：DCP CBOR 串口实现
- `MockHAL`：测试模拟

## ESP32 固件

源码 `internal/lite/urpc_agent.c`，ESP-IDF v5.x 编译。pin 范围校验（1-3），state 校验（0-1），越界返回 INVALID_CMD。

## CLI 子命令

```bash
devagent init                          # 生成 devagent.yaml + device.yaml 模板
devagent validate <device.yaml>        # 校验物模型
devagent schema <device.yaml>          # 打印能力摘要（含 intent_id）
```

## Docker

daemon 可容器化运行（mock/native 能力；串口需 CGo 镜像）：

```bash
docker compose up daemon
```

## 完整运行指南

见 [docs/RUNNING.md](docs/RUNNING.md)（含真实 ESP32 烧录、FAQ、验证清单）。

## 项目结构

```
devagent/
├── cmd/
│   ├── devagent/main.go              # CLI (slog + 优雅退出, init/validate/schema)
│   └── integration_test/main.go      # 集成测试 (Sidecar+Daemon+DCP+Token)
├── internal/
│   ├── model/                        # 数据模型 + 并发路由表 (unit/min/max/intent_id)
│   ├── mcptool/                      # 设备能力 → MCP Tool 编译 (sidecar/daemon 共用)
│   ├── sidecar/                      # Server, Router, 去重, 进度, Token
│   ├── daemon/                       # Server, Registry, DCPBridge, SerialBridge, MockHAL
│   ├── protocol/                     # uRPC, DCP(CBOR), SSE 消息
│   ├── auth/                         # HMAC Capability Token
│   ├── version/                      # 版本常量
│   └── lite/                         # ESP32 uRPC 固件 (ESP-IDF 工程)
├── configs/
│   ├── example_device.yaml           # MCU uRPC 设备
│   ├── mock_device.yaml              # 无硬件模拟设备
│   ├── pc_device.yaml                # PC 直连设备
│   ├── demo-sidecar.yaml             # demo 脚本专用 sidecar 配置（静态网关）
│   └── devagent.yaml                 # 全局配置 (token/static_gateways)
├── scripts/                          # demo.ps1 / demo.sh / mock_relay.sh
├── docs/                             # RUNNING / device-model / security / openwrt / dcp-firmware
├── deploy/                           # systemd unit + logrotate
├── Dockerfile / docker-compose.yml
├── Makefile
└── go.mod / go.sum
```
