## 语言
所有对话、解释、输出使用简体中文，除非用户要求其他语言。

## 构建与测试

```bash
go build ./...                          # 编译检查
go build -o bin/devagent.exe ./cmd/devagent/  # 构建二进制
go test ./... -v                        # 单元测试
go vet ./...                            # 静态检查
go run ./cmd/integration_test/          # 集成测试（不是 go test）
```

无 lint/fmt CI，无 golangci-lint。验证顺序：`go build` → `go vet` → `go test`。

## 架构

**双模式守护程序**：sidecar（AI 入口，stdio MCP）和 daemon（设备代理，SSE MCP + HTTP）。

```
AI Tool ──stdio──▶ Sidecar ──HTTP──▶ Daemon ──serial──▶ MCU
                   (mDNS发现)       (/invoke)
```

**包边界**：
- `internal/auth` — 共享 TokenManager，sidecar/daemon 通过 type alias 引用（避免循环依赖）
- `internal/model` — 纯数据模型，`RouteTable` 除外（含并发路由管理）
- `internal/mcptool` — 设备能力 → MCP Tool 编译（sidecar/daemon 共用）
- `internal/protocol` — 纯协议编解码，无 I/O
- `internal/config` — 全局配置加载 + 环境变量覆盖 + Validate()
- `internal/daemon` — Daemon 服务端，HAL 抽象 + 双协议 bridge
- `internal/sidecar` — Sidecar 服务端，mDNS 发现 + HTTP 转发
- `internal/version` — 版本常量 `version.Version`，发版只改这一处（MCP server、mDNS TXT、启动日志共用）
- `internal/lite` — ESP32 C 固件，非 Go

**不要**在 `daemon` 和 `sidecar` 之间直接引入包，它们通过 HTTP 通信。共享逻辑放 `auth`、`model`、`protocol`、`config`、`mcptool` 或 `version`。

## 关键约定

- **日志**：`slog.NewJSONHandler(os.Stderr)`，中文消息，JSON 格式
- **Token 别名模式**：`daemon/token.go` 和 `sidecar/token.go` 仅是 `auth` 包的 type alias，不改实现
- **异步调用**：Sidecar 设备调用是异步的，立即返回 `job_id`，用 `__system__.get_job_status` 轮询
- **YAML → MCP Tool**：`mcptool.CompileTool(deviceID, cap, maintenance)` 编译，`maintenance=true` 时描述加 `[维护中]` 前缀
- **IntentID**：代码用 SHA256 取前2字节。YAML 中可手动指定 `intent_id` 覆盖
- **串口互斥**：`SerialBridge`/`DCPBridge` 的 `SendURPC`/`SendDCP` 已有 `sync.Mutex`，不要在外层再加锁
- **DCP HMAC**：发送帧需手动调 `protocol.AppendDCPHMAC()`，`EncodeDCPCall` 不自动附加
- **同机发现**：sidecar 与 daemon 同机时 mDNS 多播不回环，需在全局配置 `sidecar.static_gateways` 指定网关（`Router.AddStaticGateway`）

## 配置

- 全局配置：`configs/devagent.yaml`，通过 `config.Load()` 加载；sidecar 支持 `static_gateways` 静态路由
- 设备配置：`-config` 参数指定 YAML 物模型
- 环境变量覆盖：`DEVAGENT_TOKEN_SECRET`、`DEVAGENT_LOG_LEVEL`、`DEVAGENT_TLS_CERT`、`DEVAGENT_TLS_KEY`、`DEVAGENT_STATE_PATH`
- 校验规则：token.secret ≥ 16字符、dedup_ttl > 0、heartbeat_timeout > heartbeat_interval、log_level ∈ {debug,info,warn,error}

## 测试注意

- 集成测试用 `go run` 而非 `go test`，入口在 `cmd/integration_test/main.go`
- `LoadDeviceConfig` 测试用相对路径 `../../configs/example_device.yaml`
- `MockHAL` 是手写函数注入模式，无 mock 框架
- 串口 bridge 无单元测试（依赖真实硬件），测试用 `MockHAL`
- `mcptool.CompileTool` 签名是 `(deviceID, cap, maintenance bool)`，不要漏第三个参数

## 已知问题

- `go.bug.st/serial` 依赖 CGo，`CGO_ENABLED=0` 编译时串口功能不可用（OpenWrt 部署需注意）
