# Remove Path Field: Why Store It If Always Root?

## The Question

**User's Question**: "Why have the field if it always has the same value?"

If we always write entries at `/` (root), why store `Path: "/"` in every `LogEntry`?

## Current Design

```go
type LogEntry struct {
    Commit    int64      // Commit number
    Seq       int64      // Transaction sequence
    Path      string     // Always "/" - redundant?
    Timestamp string     // Timestamp
    Diff      *ir.Node   // Root diff
}
```

**Problem**: `Path` is always `"/"`, so it's redundant storage.

## Options

### Option A: Remove Path Field

**LogEntry**:
```go
type LogEntry struct {
    Commit    int64      // Commit number
    Seq       int64      // Transaction sequence
    Timestamp string     // Timestamp
    Diff      *ir.Node   // Root diff (always at "/")
}
```

**Benefits**:
- Less storage (no redundant field)
- Simpler structure
- No ambiguity (always root)

**Costs**:
- Need to infer path when reading (always `/`)
- Recovery: Need to traverse diff to find paths (but we already do this for indexing)

### Option B: Keep Path Field (For Recovery/Debugging)

**Reasons to keep**:
1. **Recovery**: When rebuilding index from logs, we know entry is at `/`
2. **Debugging**: Easier to see in logs that entry is at root
3. **Future-proofing**: If we ever change design, field is already there

**But**: If it's always `/`, we can just assume it's `/` when reading.

### Option C: Store Affected Paths Instead

**LogEntry**:
```go
type LogEntry struct {
    Commit    int64      // Commit number
    Seq       int64      // Transaction sequence
    Timestamp string     // Timestamp
    Diff      *ir.Node   // Root diff
    Paths     []string   // Paths affected by this entry (for recovery)
}
```

**But**: This duplicates information (paths are in diff structure).

## Analysis: Do We Need Path for Recovery?

### Recovery Scenario

**Rebuilding index from logs**:
1. Read log entries sequentially
2. For each entry:
   - Entry is always at `/` (we know this)
   - Traverse diff structure to find all paths
   - Index each path to this entry with ExtractPath

**Example**:
```go
// Entry: {Diff: {a: {b: {y: 2}, c: {z: 3}}}}
// Traverse diff to find paths:
//   - "/a" → ExtractPath = "a"
//   - "/a/b" → ExtractPath = "a.b"
//   - "/a/c" → ExtractPath = "a.c"
// Index all paths to this entry
```

**Do we need Path field?** No - we can infer it's always `/`.

**Do we need to store paths?** No - we can traverse the diff structure.

## Recommendation: Remove Path Field

**Why**:
1. Always `/`, so redundant
2. Can infer from context (always root)
3. Less storage
4. Simpler structure

**Updated LogEntry**:
```go
type LogEntry struct {
    Commit    int64      // Commit number (set when appended to log)
    Seq       int64      // Transaction sequence (txSeq)
    Timestamp string     // Timestamp
    Diff      *ir.Node   // Root diff (always at "/")
}
```

**When Reading**:
- Always assume entry is at `/`
- Extract paths from diff structure when needed

**Recovery**:
- Read entry (no Path field, assume `/`)
- Traverse diff to find all paths
- Index each path to entry with ExtractPath

## Alternative: Keep Path for Explicitness

**If we want to be explicit**:
- Keep `Path: "/"` for clarity
- But it's redundant storage

**Trade-off**: Explicitness vs. storage efficiency.

## Answer: Remove Path Field ✅

**Reasoning**: Always `/`, so redundant. Can infer from context. Less storage, simpler structure.

**Updated LogEntry**:
```go
type LogEntry struct {
    Commit    int64      // Commit number
    Seq       int64      // Transaction sequence
    Timestamp string     // Timestamp
    Diff      *ir.Node   // Root diff (always at "/")
}
```

**When Reading**:
- Always assume entry is at `/`
- Extract paths from diff structure when needed

**Recovery**:
- Read entry (no Path field, assume `/`)
- Traverse diff to find all paths
- Index each path to entry with ExtractPath

**Benefits**:
- Less storage (no redundant field)
- Simpler structure
- No ambiguity (always root)
