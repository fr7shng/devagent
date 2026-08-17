# devagent

> **Connect any device to your AI.** A lightweight Go bridge that lets LLMs discover and control LAN devices — bare-metal MCUs, PCs, OpenWrt routers — over the [MCP](https://modelcontextprotocol.io) protocol.

[![CI](https://github.com/fr7shng/devagent/actions/workflows/ci.yml/badge.svg)](https://github.com/fr7shng/devagent/actions)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.26-blue)](go.mod)

<p align="center">
  <img src="docs/demo.gif" alt="AI controlling an ESP32 relay via devagent" width="640"/>
</p>

Ask your AI to flip a relay on an ESP32, read a sensor, or run a command on your router — devagent turns the request into a typed MCP tool call, discovers the right device over mDNS, and drives it over serial with a protocol that fits its flash size.

## Why devagent

Most "AI + hardware" stacks run through a cloud platform. devagent is the opposite: a **single static binary**, **zero-config** (mDNS discovery), and **local-first**. It speaks two serial protocols so it can reach anything from an 8 KB-flash STM32 to a full OpenWrt router.

| | devagent | Home Assistant + MCP | Raw MQTT + custom MCP |
|---|---|---|---|
| Target | bare-metal MCUs, DIY hardware, routers | smart-home ecosystem | anything with MQTT |
| Deployment | one binary, no cloud | full server stack | needs a broker + adapter |
| Device discovery | automatic (mDNS) | manual pairing | manual topics |
| MCU overhead | ~2.7 KB SRAM / <5 KB flash | n/a | n/a |

## Highlights

- **Dynamic MCP tools** — devices register/unregister tools (`{device}.{capability}`) as they come online/offline; maintenance state is surfaced to the AI.
- **Dual serial protocol** — `uRPC` (6-byte frames, CRC8, for tiny MCUs) and `DCP` (CBOR + HMAC, for ESP32-class devices).
- **Zero-config discovery** — daemons broadcast over mDNS; the sidecar finds them and their devices automatically.
- **Capability-scoped security** — HMAC-signed capability tokens, per-device-per-capability granularity or `*`.
- **Async device calls** — tools return a `job_id` immediately; poll with `__system__.get_job_status`.
- **Cross-platform** — Windows, macOS, Linux, OpenWrt (mipsle), plus an ESP-IDF firmware under `internal/lite`.

## Architecture

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

- **Sidecar** — the AI's single entry point. Stdio MCP server, mDNS discovery, dynamic tool registration, dedup + async job tracking.
- **Daemon** — runs next to the hardware (PC, router, gateway). Loads a YAML device model, registers the device's MCP tools, and dispatches invocations to the matching bridge.
- **Lite** — ESP-IDF firmware (`internal/lite`) implementing the uRPC agent with pin/state range checks.

## Quick start (5 minutes)

### 1. Build

```bash
# requires Go 1.26+
make build              # or: go build -o bin/devagent ./cmd/devagent/
```

### 2. Run a daemon with a device model

```bash
./bin/devagent -mode daemon -port 8081 -config configs/example_device.yaml -gateway-id gw_01
```

`configs/example_device.yaml` describes a relay + temperature device proxied over a serial port. It broadcasts over mDNS as `_devagent._tcp`.

**No hardware?** Use the mock device — the whole chain runs without a serial port:

```bash
./bin/devagent -mode daemon -port 8082 -config configs/mock_device.yaml -gateway-id gw_mock
```

Or try the one-command demo: `./scripts/demo.sh` (Linux/macOS) or `.\scripts\demo.ps1` (Windows).

### 3. Run the sidecar

```bash
./bin/devagent -mode sidecar
```

The sidecar discovers the daemon, fetches `/devices`, and registers tools like `shelf_01.set_relay`.

### 4. Connect your AI

Point an MCP client at the sidecar (stdio):

```json
{
  "mcpServers": {
    "devagent": { "command": "devagent", "args": ["-mode", "sidecar"] }
  }
}
```

Now ask: *"turn relay 1 on for shelf_01"*. The AI sees `shelf_01.set_relay`, and the call flows Sidecar → HTTP → Daemon → SerialBridge → ESP32 ACK.

### Built-in tools

| Tool | Purpose |
|------|---------|
| `__system__.list_devices` | list registered devices and status |
| `__system__.diagnose_connectivity` | check the full link to a device |
| `__system__.get_device_schema` | get a device's capability schema |
| `__system__.get_job_status` | poll an async job by `job_id` |

## Device model (YAML)

Capabilities are declared in YAML and compiled to MCP tools with input validation (enum/min/max/unit). `implementation.protocol` selects `uRPC` or `DCP`; `native: "shell"` runs a local command instead.

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
      channel: "/dev/ttyUSB0"     # Linux; COM3 on Windows
      baudrate: 115200
      protocol: "uRPC"            # or "DCP"
      cmd_map:
        set_relay: { cmd: 161, fmt: "{pin} {state}" }
      timeout_ms: 5000
      retry: 3
```

See [docs/device-model.md](docs/device-model.md) for the full reference.

## Dual protocol

| | uRPC | DCP (CBOR) |
|---|---|---|
| Frame overhead | 6 bytes + | 39 bytes + |
| MCU RAM | ~2.7 KB | 0.6 KB |
| MCU flash | <5 KB | 27.6 KB |
| Integrity | CRC8 | HMAC-SHA256 |
| Value returns | no (command-only ACK) | yes (CBOR payload) |
| Best for | <8 KB flash MCUs | ESP32-class devices |

## Security

Set `token.secret` in `configs/devagent.yaml` (or `DEVAGENT_TOKEN_SECRET`). Sidecar and daemon share the secret; each sidecar request mints a short-lived capability token, and the daemon verifies it against the requested capability:

```go
tm := sidecar.NewTokenManager("my-secret")
token, _ := tm.Mint([]string{"shelf_01.set_relay"}, time.Hour)
claims, _ := tm.Verify(token)
tm.CheckCap(claims, "shelf_01.set_relay")  // true
```

Capabilities are namespaced as `{device}.{capability}`; `*` grants all. TLS is supported on the daemon HTTP server via `DEVAGENT_TLS_CERT` / `DEVAGENT_TLS_KEY`.

## Platform matrix

| Platform | Arch | Serial | mDNS | Binary |
|----------|------|--------|------|--------|
| Windows | amd64, arm64 | ✅ | ✅ native | `devagent-windows-amd64.exe` |
| Linux | amd64, arm64 | ✅ | ⚠️ avahi | `devagent-linux-amd64` |
| macOS | amd64, arm64 | ✅ | ✅ Bonjour | `devagent-darwin-arm64` |
| OpenWrt | mipsle | ⚠️ CGO toolchain | ⚠️ umdns | `devagent-openwrt-mipsle` (~5 MB UPX) |

> `go.bug.st/serial` needs CGo; with `CGO_ENABLED=0` serial is unavailable (matters for OpenWrt builds — see [docs/openwrt.md](docs/openwrt.md)).

## Project layout

```
cmd/devagent/            # CLI (sidecar / daemon modes)
cmd/integration_test/    # integration tests (go run, not go test)
internal/model/          # data models + concurrent route table
internal/mcptool/        # capability → MCP tool compiler (shared)
internal/sidecar/        # MCP server, mDNS router, dedup, async jobs
internal/daemon/         # device registry, HAL bridges, native handlers
internal/protocol/       # uRPC, DCP/CBOR, SSE message codecs
internal/lite/           # ESP32 uRPC firmware (ESP-IDF)
configs/                 # global config + device models
deploy/                  # systemd unit + logrotate
```

## Development

```bash
make build        # build the current platform binary
make test         # unit tests
make vet          # go vet
make integration  # integration tests (go run ./cmd/integration_test/)
make cross        # cross-compile matrix (Windows/Linux/macOS/OpenWrt)
```

### CLI subcommands

```bash
devagent init                          # generate devagent.yaml + device.yaml templates
devagent validate <device.yaml>        # validate a device model
devagent schema <device.yaml>          # print a capability summary (with intent ids)
```

### Docker

The daemon can run in a container (mock/native capabilities; serial needs a CGo image):

```bash
docker compose up daemon
```

## FAQ

**Can it work without mDNS?** The sidecar is where mDNS lives; if you don't want discovery, run sidecar and daemon on the same machine or pre-register routes.

**Which MCUs are supported?** Any MCU that implements the ~100-line uRPC agent (see `internal/lite`). The DCP path targets ESP32-class devices with the [DCP firmware](https://github.com/device-context-protocol/dcp).

**Why is `read_temp` only acknowledged?** The 5-byte uRPC ACK has no payload channel — uRPC is a command-only protocol for tiny MCUs. Use the DCP path when you need values back.

## License

[Apache-2.0](LICENSE). The ESP32 firmware under `internal/lite` is Apache-2.0 as well.
