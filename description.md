# Streaming/Iteration Transport Options

## Problem

MATCH/PATCH work for one-shot operations, but iteration over large collections (LIST/ITER) and streaming (WATCH) need a transport that supports:

1. **Multiplexing** - Multiple concurrent streams over one connection
2. **Read/write timeouts** - Essential for robust controllers to detect dead servers/consumers

## Transport Analysis

| Transport | Read/Write Timeouts | Multiplexing | Notes |
|-----------|---------------------|--------------|-------|
| Raw TCP + tony | ✓ | ✗ | Simple but no mux |
| WebSocket | ✓ | ✗ | Need multiple connections or custom mux |
| xap | ✓ | ✓ | Custom protocol, full control |
| gRPC | ✗ | ✓ | Context deadlines only, no per-read/write |
| HTTP/2 | ✗ | ✓ | Same timeout limitations |
| HTTP/3 (QUIC) | ✓ | ✓ | Promising - QUIC streams have deadlines |

## Key Observations

- **gRPC/HTTP/2 ruled out** for streaming: Can't timeout individual reads/writes. Controllers would hang on dead streams.

- **Cursors/pagination require server state**: Conflicts with "giant tony document" simplicity. If connections are cheap (xap, HTTP/3), just stream results directly.

- **Simple iteration model**: Open stream → server sends elements → close when done. No cursor management.

- **HTTP/3 potential**: QUIC streams support Set{Read,Write}Deadline. Multiplexing + timeouts + standard protocol. But: Go stdlib lacks native support (need quic-go), UDP can hit firewall issues.

## Open Questions

1. Should logd support multiple transports (HTTP for one-shot, WebSocket/xap for streaming)?
2. Is HTTP/3 mature enough to bet on?
3. For simple controllers, what's the minimal streaming API that covers WATCH + ITER?

## Related

- Array index stability during concurrent patches (dense [n] vs sparse {key})
- Array mutations via PATCH (already works per tony.Patch tests)