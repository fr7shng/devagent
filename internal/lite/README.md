# devagent lite — ESP32 uRPC firmware

A minimal uRPC agent for ESP32 that turns the board into an "AI-controllable" device. It listens on UART0 for uRPC command frames and drives GPIO outputs (relays).

## Hardware

- ESP32 DevKitC (or any ESP32 module)
- Up to 3 relay modules on GPIO1, GPIO2, GPIO3 (active-high)

## Wiring

| ESP32 GPIO | Connection |
|-----------|------------|
| GPIO0 (UART0 TX) | daemon serial RX |
| GPIO1 (UART0 RX) | daemon serial TX |
| GPIO1 | Relay 1 IN (also UART RX — see note below) |
| GPIO2 | Relay 2 IN |
| GPIO3 | Relay 3 IN |

> **Note:** UART0 is shared between the daemon serial link and the relay GPIOs. On the ESP32 DevKitC this is fine because UART RX/TX and the relay inputs are on the same pins only if you wire them accordingly. If you prefer a cleaner split, change `UART_NUM_0` to `UART_NUM_2` in `main/urpc_agent.c` (GPIO16/17) and use GPIO1/2/3 purely for relays.

## Build & flash

Requires [ESP-IDF v5.x](https://docs.espressif.com/projects/esp-idf/).

```bash
cd internal/lite
idf.py set-target esp32
idf.py build
idf.py -p /dev/ttyUSB0 flash monitor
```

## uRPC frame

```
request: [0xAA] [Seq] [Cmd] [Len] [Payload:N] [CRC8]
ack:     [0xBB] [Seq] [Status] [QueueDepth] [CRC8]
```

Status: `0x00` OK / `0x01` BUSY / `0x02` INVALID_CMD / `0xFF` ERROR

| Cmd | Name | Payload | Behavior |
|-----|------|---------|----------|
| `0xA1` | set_relay | `[pin] [state]` | Set relay `pin` (1-3) to `state` (0/1). Out-of-range → `0x02`. |
| `0xB1` | read_temp | — | Acknowledged only (the 5-byte uRPC ACK has no data channel; use DCP for value-returning intents). |

## Matching device model

The firmware above matches `configs/example_device.yaml`:

```yaml
capabilities:
  - name: set_relay
    implementation:
      protocol: "uRPC"
      cmd_map:
        set_relay: { cmd: 161, fmt: "{pin} {state}" }
  - name: read_temp
    implementation:
      protocol: "uRPC"
      cmd_map:
        read_temp: { cmd: 177, fmt: "" }
```
