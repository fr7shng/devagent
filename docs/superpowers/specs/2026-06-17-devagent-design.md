# devagent 设计规格

> 轻量设备守护程序，装上即用，AI 通过 MCP 协议直接发现和控制局域网设备。

## 1. 项目定位

独立开源项目。一个单进程守护程序，安装到设备上后，AI 工具（Claude Code / Codex 等）通过 MCP 协议直接发现和控制该设备。覆盖电脑、智能网关、单片机（ESP32/STM32）全类型设备。

## 2. 核心原则

- **零依赖部署**：单二进制文件，无运行时依赖，装上就跑
- **MCP 直出**：每个 devagent 就是一个 MCP Server，AI 工具原生发现和调用
- **声明式物模型**：设备通过 YAML 描述自己的能力，AI 自动理解
- **裸机代理**：无法跑 devagent 的裸机单片机，通过网关上的 devagent 代理暴露

## 3. 整体架构

```
┌─────────────────────────────────────────────────────┐
│                    AI 工具 (Claude Code / Codex)      │
│                  MCP Client (stdio)                  │
└────────────┬────────────────────────────────────────┘
             │ stdio (单连接)
             ▼
┌─────────────────────────────────────────────────────┐
│           devagent (本地伴生 Sidecar)                  │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐  │
│  │ MCP Server│  │ mDNS 发现  │  │ 远程 Agent 代理   │  │
│  │ (stdio)   │  │ (Zeroconf)│  │ (SSE/HTTP 转发)  │  │
│  └──────────┘  └───────────┘  └──────────────────┘  │
│  ┌──────────┐  ┌───────────┐  ┌──────────────────┐  │
│  │ Tool 编译器│  │ 去重窗口   │  │ __system__ Probe │  │
│  │ (YAML→MCP)│  │ (2-3s)    │  │ 诊断工具         │  │
│  └──────────┘  └───────────┘  └──────────────────┘  │
└────┬─────────────────┬──────────────────────────────┘
     │ mDNS + SSE      │ mDNS + SSE
     ▼                 ▼
┌──────────────┐  ┌──────────────────────────────────┐
│ devagent     │  │ devagent (网关 Daemon)             │
│ (电脑 Daemon)│  │ ┌────────────────────────────┐    │
│              │  │ │ 虚拟设备注册表               │    │
│ 本地 HAL     │  │ │ shelf_01.read_temp          │    │
│ (GPIO/SSH等) │  │ │ shelf_02.set_relay          │    │
│              │  │ │ 心跳路由表 → 动态上下架       │    │
│              │  │ └────────────────────────────┘    │
│              │  │ 串口/MQTT ──► 裸机 MCU (STM32)    │
│              │  │ uRPC 指令    {"cmd":"gpio","args":[1,0]}│
└──────────────┘  └──────────────────────────────────┘
                         │ mDNS + SSE
                         ▼
                  ┌──────────────┐
                  │ devagent     │
                  │ (ESP32 轻端)  │
                  │ uRPC 转发层   │
                  │ (无MCP解析)   │
                  └──────────────┘
```

## 4. 三种部署形态

| 形态 | 运行环境 | MCP 协议 | 职责 |
|------|---------|---------|------|
| **Sidecar** | AI 工具所在电脑 | stdio | 本地控制 + mDNS 发现 + 远程代理转发 |
| **Daemon** | 电脑/网关/Linux | SSE/HTTP | 独立服务，被 Sidecar 发现和调用 |
| **轻端** | ESP32/RTOS | 无MCP | uRPC 转发，被 Daemon 代理 |

## 5. Sidecar 核心设计

### 5.1 数据结构

```go
type Sidecar struct {
    mcpServer    MCPServer          // stdio MCP Server
    routeTable   *RouteTable        // device_id → gateway_url
    gateways     map[string]*GWConn // gateway_id → SSE 长连接
    toolRegistry *ToolRegistry      // 编译后的 MCP Tool 集合
    dedup        *DedupWindow       // 短窗口去重
}

type RouteTable struct {
    devices map[string]string  // device_id → gateway_url
    gwMeta  map[string]*GWMeta // gateway_id → 心跳/设备列表
    mu      sync.RWMutex
}

type GWMeta struct {
    LastHeartbeat time.Time
    Devices       []string
    Status        string // "online" | "maintenance" | "offline"
}
```

### 5.2 拓扑注册表

mDNS 只解决"谁在线"，不解决"谁管谁"。Daemon 暴露 SSE 端点，Sidecar 通过 mDNS 发现 Daemon 后主动连入建立双向 SSE 通道。Daemon 通过该通道注册管辖的设备列表。Sidecar 维护 `device_id → gateway_url` 的本地哈希路由表。

AI 调用 `shelf_01.set_relay` 时，Sidecar 查表路由，直接转发给目标网关。若查不到，立即返回 MCP 错误 `Device shelf_01 is offline or not registered`。

### 5.3 路由表刷新状态机

```
┌───────────┐  收到注册/心跳   ┌──────────┐
│  UNKNOWN  │───────────────►│  ONLINE  │
└───────────┘                └────┬─────┘
                                  │ 连续3次心跳丢失(30s)
                                  ▼
                             ┌────────────┐
                             │ MAINTENANCE │ 设备Tool标记不可用
                             └────┬───────┘
                                  │ 再3次心跳丢失(再30s)
                                  ▼
                             ┌──────────┐
                             │ OFFLINE  │ 从路由表移除, Tool下架
                             └──────────┘
                                  │ 网关重新注册
                                  └──────► ONLINE
```

Tool 动态上下架规则：
- ONLINE → MAINTENANCE：Tool 仍可见但描述加 `[维护中]`，调用返回错误
- MAINTENANCE → OFFLINE：Tool 从 MCP ListTools 移除
- 任何状态恢复 → ONLINE：Tool 重新上架

> **实现说明**：Sidecar 路由表状态迁移通过 `OnStatusChange` 回调触发，网关进入/恢复 ONLINE 或 MAINTENANCE 时对 `DevicesByGateway` 下的设备调用 `RefreshDeviceTools` 立即重编译（mcp-go `AddTool` 同名覆盖，可安全重复注册），使 `[维护中]` 前缀与 ONLINE 恢复即时生效，无需等待 AI 刷新 ListTools。

### 5.4 去重窗口

对同一 `(device_id, capability, params)` 做短窗口去重（2-3秒），避免 AI 重试导致重复指令下发到物理层。

## 6. 网关 Daemon 核心设计

### 6.1 任务状态机

```
                    ┌──────────┐
                    │  IDLE    │
                    └────┬─────┘
                         │ 收到 MCP 调用
                         ▼
                    ┌──────────┐
                    │ PARSING  │ 解析YAML, 匹配implementation
                    └────┬─────┘
                         │ 路由到物理通道
                         ▼
                    ┌──────────┐
                    │ ROUTING  │ 查 channel, 去重检查
                    └────┬─────┘
                         │ 下发 uRPC 指令
                         ▼
               ┌─────────────────┐
               │  WAITING_ACK    │ 超时计时器启动
               │  (retry_count)  │
               └────┬───────┬────┘
                    │       │
              ACK到达   超时(retry < 3)
                    │       │
                    ▼       ▼ (重试)
               ┌─────────┐  │
               │COMPLETED│──┘ (retry >= 3 → TIMEOUT_ERROR)
               └─────────┘
```

关键决策：
- ACK 超时默认 5s，重试最多 3 次（可通过 YAML `retry` 覆盖）
- 重试时检查 `queue_depth`，若超阈值则不再重试，直接返回 `DEVICE_BUSY`
- `queue_depth ≥ 8`（与固件 cmd_queue 容量对齐）或 ACK 为 `BUSY` 时即返回 `device_busy` 并携带 `retry_after_ms=2000` 背压信号
- 3 次重试失败 → 返回 `TIMEOUT` MCP 错误，不静默丢弃

### 6.2 虚拟设备注册表

网关代理多个裸机单片机时，MCP Tool 命名采用 `{物理节点ID}.{功能}`（如 `shelf_01.read_temp`），在 Tool 描述中通过 @context 明确当前操作的目标设备。

网关维护心跳路由表，记录每个 MCU 的最后在线时间、通信协议（UART 波特率 / MQTT Topic）。若心跳超时，MCP Server 将该节点的 Tools 动态下架（ListTools 实时更新），避免 AI 调用死端。

### 6.3 硬件抽象层 (HAL)

YAML 的 implementation 必须显式指定物理通道（Channel），解决多串口场景的路由问题：

```yaml
implementation:
  proxy: "uart"
  channel: "/dev/ttyUSB0"
  baudrate: 115200
  protocol: "uRPC"
  cmd_map:
    set_relay: { cmd: 0xA1, fmt: "{pin} {state}" }
  timeout_ms: 5000
  retry: 3
```

uRPC 转发层维护 `Device_ID → Serial_Port` 映射。去重窗口精确到"设备 + 物理端口"级别。

DCP 传输启用 HMAC 密钥时，Daemon 对**收到的每一帧回复强制校验 HMAC**：无 HMAC 尾或验签失败的一律视为不可信帧并触发重试（避免无 HMAC 设备回复导致流错位）。

## 7. 异步 Job 机制

长耗时操作（电机旋转、OTA 升级等）不阻塞 MCP 调用，利用 MCP 的 progress 通知机制：

1. 网关 Daemon 收到调用后，立即返回 `{ "job_id": "relay_123", "status": "pending" }`
2. 网关通过串口 uRPC 与设备交互，通过 Sidecar 的 SSE 通道向 MCP Client 发送 progress 通知
3. 任务完成后，发送最终的 completed 通知
4. Sidecar 将 job_id 与 AI 的原始 MCP RequestID 绑定，确保进度通知准确回填

## 8. uRPC 协议（ESP32 轻端）

极简二进制帧格式，目标 < 8KB SRAM：

```
请求帧:
[0xAA] [seq:1B] [cmd:1B] [len:1B] [payload:0-64B] [crc:1B]

ACK 帧:
[0xBB] [seq:1B] [status:1B] [queue_depth:1B] [crc:1B]

status: 0x00=OK, 0x01=BUSY, 0x02=INVALID_CMD, 0xFF=ERROR
```

- 最大帧长度 ~70 bytes
- ESP32 端只需一个 64B RX buffer + 8B TX buffer
- `queue_depth` 复用 ACK，不新增独立字段

水位线反馈：网关收到 `queue_depth` 超阈值时，主动降速，将后续发往该设备的 MCP 调用暂时挂起，并同步向 AI 反馈 `progress: "Device busy, retrying..."`。

**固件幂等**：ESP32 端缓存最近一次已执行指令的 ACK，宿主超时重发同一 `seq` 时直接重发缓存 ACK，不重复执行设备操作（畸形帧的错误 ACK 不缓存，避免重试被钉死）。

## 9. 声明式物模型（YAML 规范）

YAML 定义为 MCP Tool 的元数据生成器（编译源），而非运行时解析器。devagent 启动时将 YAML 编译为内存中的 Tool Registry，并将 implementation 路由到对应的硬件抽象层。

```yaml
device:
  id: "shelf_01"
  name: "货架控制器01"
  type: "mcu_proxy"        # direct | mcu_proxy | esp32_lite

capabilities:
  - name: set_relay
    description: "控制继电器通断"
    inputSchema:
      type: object
      properties:
        pin: { type: integer, enum: [1, 2, 3] }
        state: { type: boolean }
      required: [pin, state]
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"
      baudrate: 115200
      protocol: "uRPC"
      cmd_map:
        set_relay: { cmd: 0xA1, fmt: "{pin} {state}" }
      timeout_ms: 5000
      retry: 3

  - name: read_temp
    description: "读取温度传感器值"
    inputSchema:
      type: object
      properties: {}
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"
      baudrate: 115200
      protocol: "uRPC"
      cmd_map:
        read_temp: { cmd: 0xB1, fmt: "" }
      timeout_ms: 3000
      retry: 2
```

直连设备（电脑）的 implementation：

**安全说明**：Daemon 的 `/devices` 端点与 Sidecar 的 `get_device_schema` 对外暴露前都会调用 `DeviceConfig.Sanitized()` 剥离各 capability 的 HMAC 密钥，避免密钥经局域网泄露给 AI 或未授权对端。

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
        command: { type: string }
      required: [command]
    implementation:
      native: "shell"
      timeout_ms: 30000

  - name: shutdown
    description: "关机"
    inputSchema:
      type: object
      properties: {}
    implementation:
      native: "shutdown"
      timeout_ms: 10000
```

## 10. Sidecar 与 Daemon 通信协议

### 10.1 网关注册（Daemon → Sidecar）

网关 Daemon 启动后，通过 SSE 长连接向 Sidecar 注册：

```json
{
  "type": "register",
  "gateway_id": "gw_1",
  "gateway_url": "http://192.168.1.50:8080",
  "devices": [
    {
      "device_id": "shelf_01",
      "device_name": "货架控制器01",
      "device_type": "mcu_proxy",
      "capabilities": ["set_relay", "read_temp"]
    }
  ],
  "timestamp": 1718600000
}
```

心跳（每 10s）：

```json
{
  "type": "heartbeat",
  "gateway_id": "gw_1",
  "devices": ["shelf_01", "shelf_02"],
  "timestamp": 1718600010
}
```

设备变更通知（热插拔）：

```json
{
  "type": "device_update",
  "gateway_id": "gw_1",
  "added": [{"device_id": "shelf_03", "capabilities": ["set_relay"]}],
  "removed": [],
  "timestamp": 1718600020
}
```

### 10.2 指令转发（Sidecar → Daemon）

```json
{
  "type": "invoke",
  "request_id": "mcp_req_001",
  "device_id": "shelf_01",
  "capability": "set_relay",
  "params": {"pin": 1, "state": true},
  "job_id": "relay_123",
  "timestamp": 1718600030
}
```

### 10.3 异步响应（Daemon → Sidecar）

进度通知：

```json
{
  "type": "progress",
  "request_id": "mcp_req_001",
  "job_id": "relay_123",
  "status": "pending",
  "progress": 50,
  "message": "指令已下发，等待ACK",
  "timestamp": 1718600031
}
```

完成通知：

```json
{
  "type": "result",
  "request_id": "mcp_req_001",
  "job_id": "relay_123",
  "status": "completed",
  "data": {"pin": 1, "state": true},
  "timestamp": 1718600032
}
```

### 10.4 错误协议

```json
{
  "type": "error",
  "request_id": "mcp_req_001",
  "job_id": "relay_123",
  "code": "DEVICE_BUSY",
  "message": "shelf_01 队列已满，queue_depth=8",
  "retry_after_ms": 2000
}
```

错误码定义：

| 错误码 | 含义 | AI 应对 |
|--------|------|---------|
| `DEVICE_OFFLINE` | 设备心跳丢失 | 等待后重试或换设备 |
| `DEVICE_BUSY` | queue_depth 超阈值 | 等待 retry_after_ms 后重试 |
| `TIMEOUT` | 3次重试无ACK | 检查物理连接 |
| `INVALID_CMD` | MCU 不识别指令 | 检查 YAML cmd_map |
| `ROUTE_NOT_FOUND` | Sidecar 路由表无此设备 | 检查网关注册状态 |

## 11. 内置系统工具（__system__ 命名空间）

Sidecar 自动注册以下只读诊断工具，AI 可随时调用：

| 工具名 | 功能 | 返回 |
|--------|------|------|
| `__system__.list_devices` | 列出所有已注册设备及状态 | `[{device_id, gateway_id, status, capabilities}]` |
| `__system__.diagnose_connectivity` | 检查到指定设备的全链路状态 | `{sidecar→gateway: ok/timeout, gateway→device: ok/timeout, gateway_status, heartbeat_age_sec}` |

> **实现说明**：`diagnose_connectivity` 的 `gateway_to_device` 通过探测 Daemon 的 `/readyz` 得到（该端点报告串口连接与设备数），`heartbeat_age_sec` 反映路由表上次心跳距今秒数；`sidecar→gateway` 依靠健康检查结果与路由状态。
| `__system__.get_device_schema` | 获取指定设备的完整能力描述 | YAML 编译后的 JSON Schema |
| `__system__.get_job_status` | 查询异步任务状态 | `{job_id, status, progress, created_at}` |

## 12. 外网访问方案

采用 Tailscale 零配置 VPN：
- 每台设备安装 Tailscale 客户端（电脑/网关原生支持，ESP32 通过网关代理）
- Sidecar 配置 Tailscale IP，外网时自动走 VPN 隧道
- 无需额外内网穿透服务

## 13. 完整数据流

以 `set_relay(1, true)` 到 ESP32 为例：

```
AI → MCP stdio → Sidecar
  → [查路由表: shelf_01 → gw_1 SSE url]
  → [去重窗口检查: (shelf_01, set_relay, {pin:1,state:true}) 无重复]
  → SSE 转发至网关 Daemon (gw_1)
  → [网关解析 YAML: channel=/dev/ttyUSB0, protocol=uRPC]
  → [任务状态: IDLE → PARSING → ROUTING → WAITING_ACK]
  → [Sidecar 立即返回 job_id="relay_123", status="pending"]
  → uRPC: [0xAA][0x01][0xA1][0x02][0x01 0x01][CRC]
  → ESP32 ACK: [0xBB][0x01][0x00][0x02][CRC]  (OK, queue_depth=2)
  → [任务状态: WAITING_ACK → COMPLETED]
  → 网关 SSE 推送 completed 给 Sidecar
  → Sidecar MCP progress 通知 → AI 收到最终结果
```

## 14. 项目结构

```
devagent/
├── cmd/
│   └── devagent/
│       └── main.go              # 入口，根据参数启动 sidecar/daemon/lite
├── internal/
│   ├── sidecar/
│   │   ├── server.go            # MCP stdio Server
│   │   ├── router.go            # 路由表 + 网关连接管理
│   │   ├── tool_compiler.go     # YAML → MCP Tool 编译
│   │   ├── dedup.go             # 短窗口去重
│   │   └── progress.go          # job_id ↔ request_id 绑定 + progress 转发
│   ├── daemon/
│   │   ├── server.go            # SSE/HTTP MCP Server
│   │   ├── task_fsm.go          # 任务状态机
│   │   ├── device_registry.go   # 虚拟设备注册表 + 心跳路由表
│   │   ├── hal.go               # 硬件抽象层 (串口/MQTT/本地GPIO)
│   │   └── urpc_bridge.go       # uRPC 协议编解码 + 串口收发
│   ├── lite/
│   │   └── urpc_agent.c         # ESP32 uRPC 转发层 (C, <8KB SRAM)
│   ├── model/
│   │   ├── device.go            # 设备/网关数据模型
│   │   ├── capability.go        # 能力描述模型
│   │   └── job.go               # Job 异步任务模型
│   └── protocol/
│       ├── urpc.go              # uRPC 帧编解码 (Go端)
│       └── sse_transport.go     # SSE 通信层
├── configs/
│   └── example_device.yaml      # 示例物模型配置
├── go.mod
├── go.sum
└── Makefile
```

## 15. 技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| 语言 | Go (主端) + C (ESP32) | Go 跨平台单二进制，C 适配嵌入式 |
| MCP SDK | mark3labs/mcp-go | Go 生态最成熟的 MCP 实现 |
| mDNS | hashicorp/mdns | 成熟稳定，零配置发现 |
| SSE | 原生 net/http + SSE | 无需第三方库，Go 标准库足够 |
| 串口 | go.bug.st/serial | Go 串口库事实标准 |
| ESP32 框架 | ESP-IDF (C) | 比 Arduino 更底层，内存控制更精确 |
| 外网 | Tailscale | 零配置 VPN，偶尔外网场景最优 |
