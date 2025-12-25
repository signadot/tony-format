# Logd Scopes

Scopes provide Copy-on-Write (COW) isolation for logd data, enabling features like Signadot sandbox interop where isolated environments need their own view of the data without affecting the baseline.

## Overview

A scope is an isolated overlay on top of the baseline data. When you connect with a scope:
- **Reads** return baseline data layered with scope-specific changes
- **Writes** only affect the scope (baseline remains unchanged)
- **Watches** see both baseline changes and scope-specific changes

## Session Model

Scope is set at the session level during the Hello handshake:

```tony
# Baseline session (full access)
{hello: {clientId: "client-1"}}

# Scoped session (isolated)
{hello: {clientId: "client-1", scope: "sandbox-123"}}
```

### Baseline Sessions (scope = nil)

Baseline sessions have full access:
- Read/write baseline data directly
- Can delete scopes via `deleteScope` operation
- Cannot see scope-specific data

### Scoped Sessions (scope = "id")

Scoped sessions are isolated:
- Reads layer scope data on top of baseline
- Writes go to scope only (baseline unaffected)
- Watches see baseline + scope changes
- Cannot use `deleteScope` operation

## COW Semantics

### Read Path

When reading from a scoped session:

```
Final State = Baseline + Scope Overlay
```

1. Read baseline state at the given commit
2. Apply scope-specific patches on top
3. Return merged result

Example:
```tony
# Baseline has:
{users: {alice: {name: "Alice", age: 30}}}

# Scope "sandbox-1" has patch:
{users: {alice: {age: 31}}}

# Read from scope "sandbox-1" returns:
{users: {alice: {name: "Alice", age: 31}}}
```

### Write Path

When writing from a scoped session:
- Patch is stored with `ScopeID` field set
- Index entries are tagged with scope
- Baseline data is never modified

### Watch Path

Watchers in a scoped session see:
- All baseline commits (where `ScopeID == nil`)
- All commits in the matching scope (where `ScopeID == session.scope`)

Watchers in a baseline session only see:
- Baseline commits (where `ScopeID == nil`)

## API Operations

### Creating a Scope

Scopes are created implicitly on first write. No explicit creation needed.

### Deleting a Scope

Only baseline sessions can delete scopes:

```tony
{deleteScope: {scopeId: "sandbox-123"}}
```

Response:
```tony
{result: {deleteScope: {scopeId: "sandbox-123"}}}
```

This removes all index entries for the scope. The underlying log entries remain (compaction handles cleanup).

### Error Codes

- `scope_not_found` - Scope doesn't exist or has no data
- Attempting `deleteScope` from a scoped session returns `invalid_message`

## Implementation Details

### Index Filtering

`LookupRange` and `LookupWithin` accept a `scopeID` parameter:
- `scopeID == nil` → return only baseline segments (`seg.ScopeID == nil`)
- `scopeID != nil` → return baseline + matching scope segments

### Storage Layer

`ReadStateAt(path, commit, scopeID)`:
1. Query index for segments (filtered by scope)
2. Read and merge patches from baseline
3. If scoped, apply scope patches on top

### LogSegment

```go
type LogSegment struct {
    StartCommit int64
    StartTx     int64
    EndCommit   int64
    EndTx       int64
    KindedPath  string
    LogFile     string
    LogPosition int64
    ScopeID     *string  // nil = baseline, non-nil = scope
}
```

### CommitNotification

Watch notifications include scope information:

```go
type CommitNotification struct {
    Commit    int64
    TxSeq     int64
    Timestamp string
    KPaths    []string
    Patch     *ir.Node
    ScopeID   *string  // nil = baseline, non-nil = scope
}
```

## Use Cases

### Signadot Sandbox Interop

Each sandbox gets its own scope:
1. Sandbox connects with `scope: "sandbox-<id>"`
2. Sandbox can read baseline data
3. Sandbox writes are isolated
4. When sandbox is deleted, `deleteScope` cleans up

### Testing Isolation

Tests can use scopes to avoid polluting shared state:
1. Each test gets a unique scope
2. Tests can modify data freely
3. Cleanup via `deleteScope`

### Preview Environments

Preview deployments can have isolated data:
1. PR gets scope `pr-123`
2. Changes in preview don't affect production baseline
3. Easy cleanup when PR is closed

## Concurrency

- Multiple scopes can coexist
- Scopes don't interfere with each other
- Baseline writes are visible to all scoped sessions (via COW read)
- Scope writes are only visible within that scope
