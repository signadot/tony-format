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
Final State = Baseline + the scope's own writes
```

1. Read baseline state at the given commit
2. Apply the scope's own writes on top
3. Return merged result

Applying the scope's writes *last* is what makes them sticky: a later baseline write to a
path the scope has written is shadowed, while baseline writes elsewhere still show
through.

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
3. If scoped, apply the scope's layer on top

### The overlay

Step 3 used to mean *every write the scope had ever made*, replayed on every read: scope
patches are exempt from both snapshotting and compaction, so a scoped read cost the
scope's whole history where a baseline read cost only what had happened since the last
snapshot.

Each baseline snapshot now also materializes each live scope's ownership as an **overlay**
— a scope-tagged log entry holding what that scope asserts, as of that commit. A scoped
read is then

```
baseline snapshot + baseline patches since + overlay(T) + the scope's patches after T
```

which is bounded by the snapshot interval, exactly as baseline is. Nothing about the
semantics above changes; what changes is how much has to be replayed to produce them.

Two consequences worth knowing:

- **A scope's operations freeze at a snapshot.** Between snapshots a stored op is
  re-applied to the live baseline, so an operation whose result depends on what it meets
  (`!rename`, `!strdiff`, …) tracks baseline. Once folded into an overlay it is the value
  it produced. This is the same shape as baseline history degrading to snapshot
  granularity, applied forwards rather than backwards.
- **A scope holding keyed arrays the schema does not declare replays instead.** An overlay
  is built by diffing two materialized states, and a `!key` that exists only because some
  write carried the tag cannot be reproduced on those states — so the diff would key by
  position where the merge keys by identity. Declaring the key in schema (`!logd-key` or
  `!logd-auto-id`) is what makes such a scope servable.

`EnableScopeOverlay(false)` turns all of this off; a store then behaves exactly as it did
before, at the old cost. See `scope_overlay_plan.md` for the design and the measurements.

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
