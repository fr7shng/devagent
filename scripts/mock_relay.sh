#!/bin/sh
# devagent mock relay: a richer simulated relay for Linux/macOS (no real hardware).
# Usage: mock_relay.sh {pin} {state} | mock_relay.sh status
# Wire it into a device model with:
#   allowed_commands: ["mock_relay.sh"]
#   cmd_map:
#     set_relay:  { cmd: 0, fmt: "mock_relay.sh {pin} {state}" }
#     read_status: { cmd: 0, fmt: "mock_relay.sh status" }
set -u

STATE_DIR="${TMPDIR:-/tmp}/devagent_mock_relay"

if [ "${1:-}" = "status" ]; then
    for p in 1 2 3; do
        f="$STATE_DIR/$p.state"
        if [ -f "$f" ]; then
            printf 'pin %s: %s\n' "$p" "$(cat "$f")"
        else
            printf 'pin %s: off\n' "$p"
        fi
    done
    exit 0
fi

pin="${1:-}"
state="${2:-}"
if [ "$pin" != "1" ] && [ "$pin" != "2" ] && [ "$pin" != "3" ]; then
    printf 'mock relay: invalid pin %s\n' "$pin" >&2
    exit 2
fi
case "$state" in
    0|1) ;;
    *) printf 'mock relay: invalid state %s\n' "$state" >&2; exit 2 ;;
esac

mkdir -p "$STATE_DIR"
printf '%s' "$state" > "$STATE_DIR/$pin.state"
printf 'mock relay: pin=%s state=%s (simulated)\n' "$pin" "$state"
exit 0
