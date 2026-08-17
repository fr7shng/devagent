# Making an MCU controllable by LLMs via MCP

*Originally published alongside the devagent v0.4.0 release.*

LLMs are great at calling tools. Those tools usually live in the cloud — a weather API, a codebase, a browser. The gap I wanted to close is the last meter of cable: **an LLM reaching a physical device sitting on your desk, on your router, or inside a machine.** Not through a cloud platform, but directly, over a serial port.

This post is about the design decisions behind **devagent**, a single-binary Go daemon that exposes any LAN device as MCP tools.

## The shape of the problem

To make a device "AI-controllable" you need four pieces:

1. **Discovery** — how does the AI know a device exists?
2. **Schema** — how does it know what the device can do, and with what arguments?
3. **Invocation** — how does a tool call become a physical action?
4. **Safety** — how do you stop the AI from doing something destructive?

Each one has an obvious "just use X" answer (mqtt, openapi, ssh, an allowlist). The interesting part is how they interact when your target is an 8 KB-flash MCU.

## Decision 1: two processes, one binary

`devagent -mode sidecar` and `devagent -mode daemon` are the same binary. The **sidecar** is the AI's single entry point: a stdio MCP server. The **daemon** lives next to the hardware and owns the serial port. They talk over HTTP with a tiny JSON message; discovery is mDNS (`_devagent._tcp`).

Why not one process? The sidecar should be runnable on your laptop while the daemon proxies hardware on a router across the room. Why not a daemon per device type? Because the serial transport and the protocol framing are the only device-specific parts — that's exactly what the `HAL` interface isolates.

## Decision 2: dynamic MCP tools

When a daemon announces itself, the sidecar fetches `/devices`, gets each device's YAML capability model, and calls `mcpServer.AddTool` for every `{device}.{capability}`. Device goes offline (heartbeat timeout)? The tools are removed. A gateway enters maintenance? Every tool's description is prefixed `[维护中]`.

This means the model's *tool list is the live state of your network*. No hand-written manifest, no refresh step. The AI discovers capabilities the same way a human walking into the room would.

## Decision 3: two serial protocols, chosen by flash budget

An ESP32 with a few KB to spare and an STM32 with an 8 KB budget are different animals. devagent speaks both:

- **uRPC** — 6-byte request frames, CRC8, ~2.7 KB SRAM on the MCU. It is deliberately command-only: the ACK is a fixed 5 bytes with no payload channel. You cannot read a temperature back over uRPC, and that's a feature — it keeps the footprint tiny and the protocol trivially auditable.
- **DCP** — CBOR frames with HMAC-SHA256, ~0.6 KB RAM. Payloads carry return values, so value-returning intents go here.

The capability YAML picks the protocol per capability (`implementation.protocol`). The HAL interface (`SendURPC`/`SendDCP`) is 60 lines thick, so the daemon never cares which one a device speaks.

The "value-returning needs DCP" constraint is a real one for product planning, and worth stating loudly in the docs rather than papering over.

## Decision 4: async calls and a job table

MCP tool calls are synchronous by default. A relay click is fast, but a shell command or a serial retry can take seconds. So the sidecar returns immediately with a `job_id` and the AI polls `__system__.get_job_status`. This keeps the MCP session responsive and gives a natural place for dedup (identical invocations within 3 s are rejected) and progress tracking.

## Decision 5: capability-scoped tokens instead of "trust the LAN"

Everything in the sidecar→daemon direction is signed with a shared-secret HMAC. Each sidecar request mints a short-lived token whose claims are the requested capabilities, namespaced as `{device}.{capability}`. The daemon verifies signature, expiry, and the specific capability before touching hardware. Granular tokens (`shelf_01.set_relay` only) work because both sides agree on the same namespace as the MCP tool names.

## What I'd do differently next time

- **mDNS + TLS don't mix well** — discovery advertises plain HTTP; TLS mode needs manual routing. A `scheme` field in the mDNS TXT record would fix it.
- **The command-only uRPC ACK** bit us exactly once: someone will always want a value back from a tiny MCU. Plan for a small "response frame" variant from day one.
- **One binary, two personalities** is convenient but means the shared global config is loaded by both, and drift between sidecar/daemon config is easy.

## Try it

```bash
go build -o devagent ./cmd/devagent/
./devagent -mode daemon -port 8081 -config configs/example_device.yaml -gateway-id gw_01 &
./devagent -mode sidecar
```

Then point an MCP client (Claude, Codex) at the sidecar and ask it to flip a relay. The firmware for the ESP32 side is a ~140-line ESP-IDF project in `internal/lite`.

The repo: **fr7shng/devagent** — Apache-2.0.
