# OpenWrt deployment

devagent runs on OpenWrt as a **gateway daemon** that proxies downstream ESP32/MCU devices over serial. Flash is limited on routers, so the binary is stripped and compressed.

## Prerequisites

- OpenWrt SDK with the matching target toolchain (e.g. `mipsel-openwrt-linux-gcc` for mipsle)
- `umdns` installed on the router for mDNS advertisement (`opkg install umdns`)
- Serial hardware: the router's UART or a USB-UART adapter

## Build the binary

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=mipsle GOMIPS=softfloat \
  go build -ldflags="-s -w" -o bin/devagent-openwrt-mipsle ./cmd/devagent/
upx --best bin/devagent-openwrt-mipsle   # ~12 MB → ~5 MB
```

> `make cross` produces this artifact as `bin/devagent-openwrt-mipsle`.

## Install

```bash
scp bin/devagent-openwrt-mipsle root@192.168.1.1:/usr/bin/devagent
```

Create a device model for the router (control `iptables`/wifi via `native: "shell"`, and relay GPIOs via `proxy: "uart"` on `/dev/ttyS1`), then run:

```bash
devagent -mode daemon -port 8081 -config /etc/devagent/device.yaml -gateway-id gw_router
```

## systemd-style supervision

OpenWrt uses procd, not systemd:

```
/etc/init.d/devagent (procd service):
  procd_set_param command /usr/bin/devagent
  procd_set_param command_args '-mode daemon -port 8081 -config /etc/devagent/device.yaml -gateway-id gw_router'
  procd_set_param respawn 3600 5 0
```

Or use the included `deploy/devagent.service` on regular systemd Linux.

## Caveats

- `go.bug.st/serial` uses CGo. With `CGO_ENABLED=0` the serial transports are **unavailable** — the daemon will still serve `/devices` and native commands, but MCU proxy capabilities need the CGo build (native toolchain).
- If mDNS is not available, disable discovery and run the sidecar with a pre-registered route, or run sidecar and daemon on the same host.
