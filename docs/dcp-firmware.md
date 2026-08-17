# DCP firmware for the DCP transport path

devagent's `DCP` transport (CBOR frames with HMAC) targets ESP32-class devices. The daemon implements the **bridge side** of the [DCP protocol](https://github.com/device-context-protocol/dcp); the device side is the DCP firmware.

## Where the firmware lives

The DCP firmware is maintained in the DCP project itself, not in this repo:

- Protocol + Python bridge: https://github.com/device-context-protocol/dcp
- ESP32 DCP firmware: see the firmware examples in the DCP repo (`firmware/` or the README)

devagent is protocol-compatible with DCP v0.3 frame format:

```
[Ver:1] [Kind:1] [Seq:1] [IntentID:2] [Len:1] [CBOR Payload:N] [HMAC:16]
```

- Kind: `0x01` Call / `0x02` Reply / `0x03` Event / `0x04` Error / `0x81` DryRun
- IntentID = first two bytes of SHA-256 of `{device_id}.{capability}` (or set `intent_id` in YAML explicitly)

## Wiring a DCP device into devagent

1. Flash the DCP firmware to the ESP32 (follow the DCP repo instructions; wire its UART to your daemon host).
2. Declare the device with `protocol: "DCP"` in the device model:

```yaml
device:
  id: "dcp_01"
  name: "DCP 设备"
  type: "mcu_proxy"

capabilities:
  - name: set_relay
    description: "控制继电器通断"
    intent_id: 4660
    inputSchema:
      type: object
      properties:
        pin:   { type: integer, enum: [1, 2, 3], unit: "gpio_pin" }
        state: { type: boolean, unit: "on_off" }
      required: [pin, state]
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"          # Windows: COM3
      baudrate: 115200
      protocol: "DCP"
      hmac_secret: "shared-with-firmware"   # must match the firmware's secret
      timeout_ms: 5000
      retry: 3
```

3. Save the YAML above as `dcp_device.yaml`, then run the daemon:

```bash
devagent -mode daemon -port 8081 -config dcp_device.yaml -gateway-id gw_dcp
```

## Why DCP instead of uRPC

| | uRPC | DCP |
|---|---|---|
| MCU flash | <5 KB | ~27.6 KB |
| Value returns | no (command-only ACK) | yes (CBOR payload) |
| Integrity | CRC8 | HMAC-SHA256 |

Choose DCP when the device needs to **return values** (read sensor data) or wants frame-level authentication. `read_temp`-style intents require DCP; uRPC cannot carry a value back.
