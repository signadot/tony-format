# MOUNT protocol and docd bootstrapping

# MOUNT protocol and docd bootstrapping

Define the MOUNT protocol for controller registration and implement initial docd scaffolding.

## Context

Controllers register with docd via MOUNT over TCP. Once mounted, docd routes write operations (PATCH) to controllers, while reads (MATCH, WATCH) go directly to logd.

```
Controller ──TCP──▶ docd (:9090)
                      │
                      ├── PATCH → controller → logd
                      ├── MATCH → logd (direct)
                      └── WATCH → logd (direct)
```

## Connection Model

All controllers connect to docd via TCP. No stdio, no spawning.

- Controllers are independent processes
- Connect to docd, send MOUNT, receive operations
- Same protocol whether controller runs locally or remotely
- Mirrors logd session protocol design

```
# Startup sequence
1. logd starts, listens on :9091
2. docd starts, connects to logd, listens on :9090
3. Controllers start, connect to docd, send MOUNT
4. Clients connect to docd, send MATCH/PATCH/WATCH
```

## MOUNT Protocol

Session protocol over TCP, newline-delimited Tony (like logd).

### Hello + Mount (controller → docd)

```tony
hello:
  controller: user-ctrl
mount:
  path: /users
  schema:
    define:
      User:
        id: .string
        name: .string
    accept:
      users: !key(id) .array(.User)
```

### Response (docd → controller)

```tony
# Success
result:
  hello:
    docdId: docd-1
  mount:
    path: /users
    accepted: true

# Error
error:
  code: mount_failed
  message: "path /users already mounted"
```

### Operations (docd → controller, after mount)

```tony
# docd routes PATCH to controller
patch:
  id: "req-123"           # for async response matching
  path: /users
  data:
    users: !key(id)
    - !insert
        id: "123"
        name: "Alice"
```

### Response (controller → docd)

```tony
# Success - controller has written to logd
result:
  id: "req-123"
  patch:
    commit: 42
    data: {id: "123", name: "Alice"}

# Error
error:
  id: "req-123"
  code: validation_failed
  message: "name is required"
```

## docd Components

### 1. TCP Listener
Accept controller and client connections.

### 2. Session Handler
Parse messages, dispatch to appropriate handler.

### 3. Mount Registry
```go
type MountRegistry struct {
    mu     sync.RWMutex
    mounts map[string]*ControllerConn  // path → controller
}

func (r *MountRegistry) Register(path string, conn *ControllerConn) error
func (r *MountRegistry) Lookup(path string) (*ControllerConn, bool)
func (r *MountRegistry) Unregister(path string)
```

### 4. Schema Composer
Merge controller schemas into unified schema at `/.meta/schema`.

### 5. Router
- PATCH: lookup controller for path, forward, await response
- MATCH: read from logd directly
- WATCH: subscribe to logd directly

### 6. logd Client
docd maintains connection to logd for direct reads.

## Initial Scope

- TCP listener for controllers
- MOUNT handshake (hello + mount)
- Mount registry (single controller per path)
- PATCH routing to controller
- MATCH/WATCH passthrough to logd
- Basic error handling

## Out of Scope (later)

- Schema composition (just store, don't compose yet)
- Multi-controller transactions
- Controller health/reconnect
- Prefix matching for mount paths
- docd mounting another docd (#36)

## Related Issues

- #34 - docd decomposition (request routing)
- #36 - docd mounting another docd