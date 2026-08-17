# Device model reference

A device model is a YAML file passed to the daemon with `-config`. It declares the device identity and every capability the device exposes. Each capability becomes an MCP tool named `{device_id}.{capability}`.

```yaml
device:
  id: "shelf_01"
  name: "Shelf controller 01"
  type: "mcu_proxy"

capabilities:
  - name: set_relay
    description: "control a relay"
    intent_id: 4660            # DCP intent id (optional; auto-computed if omitted)
    inputSchema:
      type: object
      properties:
        pin:   { type: integer, enum: [1, 2, 3], unit: "gpio_pin" }
        state: { type: boolean, unit: "on_off" }
      required: [pin, state]
    implementation:
      proxy: "uart"
      channel: "/dev/ttyUSB0"
      baudrate: 115200
      protocol: "uRPC"
      cmd_map:
        set_relay: { cmd: 161, fmt: "{pin} {state}" }
      timeout_ms: 5000
      retry: 3
```

## `device`

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique device id; becomes the MCP tool name prefix |
| `name` | string | Human-readable name |
| `type` | string | `mcu_proxy` (serial) or `direct` (native handler) |

## `capabilities[].inputSchema`

Valid property types: `integer`, `string`, `boolean`, `number`.

| Property | Applies to | Meaning |
|----------|-----------|---------|
| `enum` | integer | Allowed values (becomes MCP `enum`) |
| `min` / `max` | integer, number | Range bounds (becomes MCP `min`/`max`) |
| `unit` | any | Appended to the tool description as `{name}({unit})` |

`required` lists property names that are mandatory.

## `capabilities[].implementation`

Three execution paths:

### 1. Serial proxy (MCU)

```yaml
implementation:
  proxy: "uart"
  channel: "/dev/ttyUSB0"     # Windows: "COM3"
  baudrate: 115200
  protocol: "uRPC"            # or "DCP"
  cmd_map:
    set_relay: { cmd: 161, fmt: "{pin} {state}" }
  timeout_ms: 5000
  retry: 3
```

- `protocol: "uRPC"` → `SerialBridge`. The `cmd` is the frame command byte; `fmt` is a template where `{param}` placeholders are replaced with values (integers/booleans/strings).
- `protocol: "DCP"` → `DCPBridge`. `intent_id` (or the auto-computed hash of `{device}.{capability}`) is used as the DCP intent id; `cmd_map` is ignored. Set `hmac_secret` to enable HMAC-SHA256 signing of DCP frames (the firmware must share the same secret).

### 2. Native shell (direct device)

```yaml
implementation:
  native: "shell"
  timeout_ms: 30000
```

Executes the `command` parameter locally. Use `allowed_commands` (list of base commands) to whitelist, otherwise the built-in dangerous-character check applies.

### 3. Proxy + native

A single capability can have only one implementation path; give the device separate capabilities if it needs both.

## Intent id

For DCP devices, `intent_id` is computed as the first two bytes of the SHA-256 of `{device_id}.{capability_name}` unless you set it explicitly in YAML. Keep it in sync with the firmware if the firmware hard-codes intents.
