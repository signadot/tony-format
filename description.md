# Async Controller Session Architecture

This issue blocks #101 and covers the design for async controller session architecture.

# Problem Statement

Controllers need bidirectional async communication with both docd and logd:
- **docd → controller**: PATCH requests routed by mount path
- **controller → docd**: PATCH responses
- **controller ↔ logd**: Match/Patch/Watch with ID-based async, tx blocking, reconnect with replay

Key constraints:
1. Tx writes to logd can block waiting for multi-participant commit
2. Connection failures during blocked tx need replay from known commit
3. Interface should hide async complexity from controller logic
4. Must be rock-solid - design once, use forever

# Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                     Controller Logic                         │
│  (simple sync-like interface: HandlePatch, Write, Query)    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Session Coordinator                       │
│  - Manages both docd and logd sessions                      │
│  - Routes PATCH requests to handler                         │
│  - Tracks pending operations with commit positions          │
│  - Handles reconnection with replay                         │
└─────────────────────────────────────────────────────────────┘
           │                                    │
           ▼                                    ▼
┌─────────────────────┐            ┌─────────────────────────┐
│   DocdSession       │            │     LogdSession         │
│ - Mount handshake   │            │ - Hello handshake       │
│ - Receive PATCH     │            │ - ID-based async        │
│ - Send responses    │            │ - Watch with replay     │
│ - Reconnect         │            │ - Tx coordination       │
└─────────────────────┘            └─────────────────────────┘
```

# Component Design

## 1. LogdSession (Async)

**Location:** system/libctl/logd_session.go

```go
type LogdSession struct {
    addr     string
    clientID string

    // Connection state
    mu        sync.Mutex
    conn      net.Conn
    decoder   *stream.Decoder
    connected bool

    // Async request tracking
    nextID    atomic.Uint64
    pending   map[string]pendingRequest  // ID -> pending
    pendingMu sync.Mutex

    // Watch tracking
    watches   map[string]*Watch  // path -> watch
    watchesMu sync.RWMutex

    // Background goroutines
    outgoing  chan outgoingMessage
    done      chan struct{}
    wg        sync.WaitGroup
}
```

**Goroutine Structure:**
- **writer**: Reads from outgoing channel, writes to connection
- **reader**: Reads responses, routes to pending requests or watch handlers
- **reconnector**: Monitors connection health, triggers reconnect with watch replay

## 2. docd↔Controller Protocol (Post-Mount)

After mount handshake, docd sends PATCH requests and controller sends responses.

**docd → Controller:** {id: "req-123", patch: {path: "/users/1", data: {name: "Alice"}}}
**Controller → docd (Success):** {id: "req-123", result: {commit: 42, data: {...}}}
**Controller → docd (Error):** {id: "req-123", error: {code: "match_failed", message: "..."}}

## 3. DocdSession (Controller Side)

**Location:** system/libctl/docd_session.go

Handles incoming PATCH requests from docd and outgoing responses via channels.

## 4. Session Coordinator

**Location:** system/libctl/coordinator.go

Ties DocdSession and LogdSession together, dispatches PATCHes to handler, tracks pending ops.

## 5. Simple Controller Interface

**Location:** system/libctl/controller.go

```go
func RunController(ctx context.Context, cfg *ControllerConfig) error
```

# Reconnection & Replay Strategy

## LogdSession Reconnect
1. Detect failure (reader error)
2. Mark disconnected
3. Reconnect with exponential backoff (100ms → 5s)
4. Re-establish watches with fromCommit = watch.LastCommit
5. Fail pending requests with connection error
6. Resume writer/reader goroutines

## DocdSession Reconnect
1. Detect failure, reconnect with backoff
2. Re-mount (hello + mount)
3. Notify coordinator for pending op replay

# Error Handling

| Error | Handling |
|-------|----------|
| Connection lost (logd) | Reconnect with backoff, pending requests get error |
| Connection lost (docd) | Reconnect with backoff, re-mount |
| Tx timeout | Return error to caller |
| Controller crash mid-tx | docd returns "outcome unknown" to client |

# Design Decisions

1. **Watch Reconnect: Manual** - Watches don't auto-restart; caller re-watches with WithFromCommit(lastCommit)
2. **Recovery: In-Memory** - No persistence, but guaranteed "outcome unknown" visibility on crash
3. **Sync-Looking API** - Methods block internally while using async ID-based protocol

# Implementation Order

1. Protocol types (docd/api/controller.go)
2. Async LogdSession with ID-based tracking
3. DocdSession for controller side
4. Session Coordinator
5. Controller interface
6. docd server updates for PATCH routing
7. Tests