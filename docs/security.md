# Security model

devagent uses capability-scoped HMAC tokens between the sidecar and the daemon. There is no cloud, no account, and no phone-home.

## Token format

A token is `base64(claims).base64(hmac_sha256(secret, base64(claims)))`:

```
eyJjYXBzIjpbIioiXSwiZXhwIjoxNzEwMTIzfQ.1oZ9...
```

Claims: `{ "caps": [...], "exp": <unix seconds> }`.

## Flow

1. Both sidecar and daemon share a secret (`token.secret` in `configs/devagent.yaml`, or `DEVAGENT_TOKEN_SECRET`).
2. The sidecar mints a short-lived (5 min) token for each `/invoke` request with caps `["*"]` (or a tighter cap set).
3. The daemon verifies the signature, expiry, and that the requested capability is allowed via `CheckCap`.

## Capability granularity

Capabilities are namespaced as `{device_id}.{capability}` — the same string as the MCP tool name. Examples:

```go
tm.Mint([]string{"shelf_01.set_relay"}, time.Hour)   // only that device's relay
tm.Mint([]string{"shelf_01.*"}, time.Hour)           // every capability of shelf_01
tm.Mint([]string{"*"}, time.Hour)                    // everything
```

`CheckCap` compares against `{device}.{capability}` (the daemon joins the request's device id and capability before checking).

## Other defenses

- **Rate limiting** — the daemon limits `/invoke` to 30 req/s per client IP.
- **Dedup window** — the sidecar rejects identical invocations within `dedup_ttl` (default 3 s).
- **Serial frame integrity** — uRPC uses CRC8; DCP optionally appends an HMAC-SHA256 trailer.
- **TLS** — set `DEVAGENT_TLS_CERT`/`DEVAGENT_TLS_KEY` to serve the daemon over HTTPS. Note that mDNS advertises the plain HTTP URL; for TLS, pin the sidecar route manually.
- **Native command guard** — `native: "shell"` capabilities can restrict execution with `allowed_commands`; a dangerous-character check rejects shell metacharacters (`;`, `|`, `&`, `` ` ``, `>`, `<`, `&&`, `||`).

## Secret rotation

Change the secret, restart both processes. Old tokens fail signature verification immediately. Because both sides must share the secret, distribute it out-of-band (e.g. config management, environment variable) when sidecar and daemon run on different machines.
