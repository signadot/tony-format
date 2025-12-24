# Session Protocol (TCP)

The session protocol provides bidirectional streaming communication over TCP for real-time watches and request pipelining. It complements the HTTP protocol for use cases requiring persistent connections and server-push events.

## Connection

Connect via TCP to the logd server's session port (configured with `-tcp` flag):

```bash
o logd session localhost:9123
```

Messages are newline-delimited Tony documents in wire format (single line).

## Message Format

### Request Structure

All requests share a common structure:

```tony
# Synchronous request (client blocks until response)
hello: { clientId: "my-client" }

# Asynchronous request (client can pipeline, match responses by ID)
id: "req-1"
match: { body: { path: "/users" } }
```

The optional `id` field enables request pipelining - responses include the same ID for correlation.

### Response Structure

Responses are one of three types:

```tony
# Result (success response to a request)
id: "req-1"
result: { match: { commit: 42, body: { ... } } }

# Event (streaming watch data)
event: { commit: 43, path: "/users", patch: { ... } }

# Error
id: "req-1"
error: { code: "invalid_path", message: "path must start with /" }
```

## Operations

### Hello (Handshake)

Initial handshake to establish session:

```tony
# Request
hello: { clientId: "my-client-id" }

# Response
result: { hello: { serverId: "session-abc123" } }
```

### Match (Read)

Read state at a path:

```tony
# Request - read all users
id: "1"
match: { body: { path: "/users" } }

# Response
id: "1"
result: { match: { commit: 42, body: { users: [...] } } }
```

With filter:

```tony
# Request - read specific user
id: "2"
match: { body: { path: "/users", data: { id: "user-1" } } }

# Response
id: "2"
result: { match: { commit: 42, body: { id: "user-1", name: "Alice" } } }
```

### Patch (Write)

Apply a patch to the data:

```tony
# Request
id: "3"
patch: { patch: { path: "/users", data: !arraydiff [ !insert { id: "user-2", name: "Bob" } ] } }

# Response
id: "3"
result: { patch: { commit: 43 } }
```

### Watch

Watch for changes at a path:

```tony
# Request - watch /users from current commit
watch: { path: "/users" }

# Response (confirmation)
result: { watch: { watching: "/users" } }

# Events (streamed as changes occur)
event: { commit: 44, path: "/users", patch: { ... } }
event: { commit: 45, path: "/users", patch: { ... } }
```

With replay from a specific commit:

```tony
# Request - watch with replay from commit 40, include full state
watch: { path: "/users", fromCommit: 40, fullState: true }

# Response (indicates replay is happening)
result: { watch: { watching: "/users", replayingTo: 43 } }

# First event is full state at commit 40
event: { commit: 40, path: "/users", state: { users: [...] } }

# Historical patches (41, 42, 43) are replayed
event: { commit: 41, path: "/users", patch: { ... } }
event: { commit: 42, path: "/users", patch: { ... } }
event: { commit: 43, path: "/users", patch: { ... } }

# Replay complete marker
event: { replayComplete: true }

# Live events continue
event: { commit: 44, path: "/users", patch: { ... } }
```

### Unwatch

Stop receiving events for a path:

```tony
# Request
unwatch: { path: "/users" }

# Response
result: { unwatch: { unwatched: "/users" } }
```

## Error Codes

| Code | Description |
|------|-------------|
| `invalid_message` | Malformed request |
| `invalid_path` | Path validation failed |
| `invalid_diff` | Patch data missing or invalid |
| `already_watching` | Already watching this path |
| `not_watching` | Not watching this path |
| `commit_not_found` | Requested commit not available |
| `session_closed` | Session terminated (e.g., slow consumer) |

## Sync vs Async Mode

**Synchronous** (no `id` field): Client sends request and waits for response. Simple but no pipelining.

**Asynchronous** (with `id` field): Client can send multiple requests without waiting. Responses include the `id` for correlation. Enables request pipelining for higher throughput.

Events from watches are always asynchronous and do not have an `id`.

## Replay and Race Prevention

When watching with `fromCommit`, the server:

1. Registers with the watch hub first (queuing live events)
2. Gets the current commit as replay target
3. Sends full state at `fromCommit` (if `fullState: true`)
4. Replays historical patches from `fromCommit+1` to current
5. Sends `replayComplete` marker
6. Forwards live events, deduplicating any that overlap with replay

This ensures no events are missed during the watch setup.

## Example Session

```bash
$ o logd session localhost:9123
Connected to localhost:9123

# Input (type these)
hello: { clientId: "demo" }
id: "1"
match: { body: { path: "/config" } }
watch: { path: "/config" }
id: "2"
patch: { patch: { path: "/config", data: { debug: true } } }

# Output (server responses)
result: { hello: { serverId: "session-abc" } }
id: "1"
result: { match: { commit: 10, body: { timeout: 30 } } }
result: { watch: { watching: "/config" } }
id: "2"
result: { patch: { commit: 11 } }
event: { commit: 11, path: "/config", patch: { debug: true } }
```

## Related Documentation

- [HTTP Protocols](./http-protocols.md) - HTTP request/response formats (MATCH, PATCH, WATCH)
- [Subscription Formats](./subscriptions.md) - WATCH semantics and diff streaming
- [logd Server Design](./logd-server-design.md) - Storage architecture
