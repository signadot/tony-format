# Log Entry Schema Critique

## Current State

### Current LogEntry Schema
```go
type LogEntry struct {
    Commit    int64      // Commit number
    Seq       int64      // Transaction sequence
    Path      string     // Kinded path (syntax encodes kind: a.b, a[0], a{0})
    Timestamp string     // Timestamp
    Diff      *ir.Node   // The actual diff
    Pending   bool       // Is this entry pending?
}
```

### Current Transaction Commit Flow
1. **Allocate commit number** (`NextCommit()`)
2. **Write pending files** (with `commit=0`, `Pending=true`)
3. **Commit files** (rename `.pending` to `.diff`, update index with actual commit)
4. **Write transaction log** (separate log entry with commit number)

## Critique Points

### 1. Path as String vs Recursive Struct

**Current**: Path is stored as a string (`"a.b[0]"`)

**Problem**: 
- Path parsing is required every time we need to extract kind information
- No type safety - easy to have malformed paths
- Cannot leverage Tony schema's recursive structure capabilities
- Kind information is encoded in syntax but not structured

**Proposal**: Path should be a recursive Go struct/Tony schema
```go
//tony:schemagen=path-segment
type PathSegment struct {
    Key  string      // The key/index (e.g., "a", "0", "42")
    Kind ir.Type     // The kind of this segment (ObjectType, ArrayType, SparseArrayType)
    Next *PathSegment // Next segment in path (nil for leaf)
}

//tony:schemagen=log-entry
type LogEntry struct {
    Commit    int64
    Seq       int64
    Path      *PathSegment  // Recursive path structure
    Timestamp string
    Diff      *ir.Node
    Pending   bool
}
```

**Benefits**:
- Type-safe path representation
- Kind information directly accessible without parsing
- Natural recursive structure matches hierarchy
- Can leverage Tony schema's recursive capabilities
- Easier to traverse/manipulate paths programmatically

**Trade-offs**:
- Slightly larger storage (but Tony wire format is efficient)
- Need path parsing/construction utilities (but we already need this for kinded paths)

### 2. Transaction Write Timing

**Current**: Two-step process
1. Write pending files (`commit=0`, `Pending=true`)
2. Commit files (rename, update index with actual commit)
3. Write transaction log separately

**Problem**:
- Race condition window: files exist with `commit=0` before being committed
- Recovery complexity: need to handle partial commits
- Index inconsistency: entries might reference `commit=0` temporarily
- Two separate write operations (files + transaction log)

**Proposal**: Write log entries exactly when committed
- Allocate commit number
- Write all log entries to `level0.log` atomically with `Commit` set and `Pending=false`
- Update index atomically with commit number
- No separate "pending" vs "committed" files - just `Pending` flag in log entry

**Benefits**:
- Single atomic write operation per commit
- No race conditions - commit number is set from the start
- Simpler recovery - entries are either committed or not (no rename step)
- Index consistency - entries always have correct commit number
- Matches LSM-style append-only log pattern

**Implementation**:
```go
// Allocate commit number
commit, err := storage.NextCommit()

// Write all log entries atomically
for _, diff := range diffs {
    entry := &LogEntry{
        Commit:    commit,  // Set commit from the start
        Seq:       txSeq,
        Path:      parseKindedPath(diff.Path),  // Recursive struct
        Timestamp: timestamp,
        Diff:      diff.Node,
        Pending:   false,  // Committed immediately
    }
    // Append to level0.log, get LogPosition
    logPosition := appendToLog(entry)
    // Update index atomically
    index.Add(path, level0, logPosition, commit, commit, txSeq, txSeq)
}
```

**Trade-offs**:
- Need to handle rollback if write fails (but we already need this)
- Cannot "uncommit" easily (but this is probably fine - commits are final)

### 3. Pending Flag Necessity

**Question**: If we write entries exactly when committed, do we need `Pending` flag?

**Current Use Case**: 
- Pending files written with `commit=0`, then renamed/committed
- Transaction log tracks pending files

**With New Approach**:
- If entries written exactly when committed, `Pending=false` always
- **BUT**: What about multi-step transactions that need to be visible before final commit?
- **OR**: What about read-your-writes within a transaction?

**Options**:
1. **Remove `Pending` flag**: If all writes are committed immediately
2. **Keep `Pending` flag**: For future multi-step transaction support
3. **Separate transaction log**: Keep separate log for transaction state, storage log only has committed entries

**Recommendation**: Keep `Pending` flag for now, but set to `false` for initial implementation. Allows future extension without schema change.

## Revised Schema Proposal

```go
//tony:schemagen=path-segment
type PathSegment struct {
    Key  string       // The key/index (Tony string representation)
    Kind ir.Type      // ObjectType, ArrayType, SparseArrayType, etc.
    Next *PathSegment // Next segment (nil for leaf)
}

//tony:schemagen=log-entry
type LogEntry struct {
    Commit    int64         // Commit number (set when written, not 0)
    Seq       int64         // Transaction sequence
    Path      *PathSegment  // Recursive path structure
    Timestamp string        // Timestamp
    Diff      *ir.Node      // The actual diff
    Pending   bool          // Reserved for future use (currently always false)
}
```

## Implementation Changes Needed

1. **Path Parsing**: Create `ParseKindedPath(string) *PathSegment` function
2. **Path Serialization**: Create `PathSegment.String() string` method
3. **Write Flow**: Change to write log entries atomically with commit number
4. **Recovery**: Update recovery to handle new schema (no rename step needed)

## Open Questions

1. **Path Segment Key Type**: Should `Key` be `string` or `*ir.Node`? (String is simpler, but Node allows more complex keys)
2. **Path Comparison**: How do we compare paths efficiently for indexing? (String representation vs structural comparison)
3. **Backward Compatibility**: How do we migrate from string paths to recursive paths? (One-time migration on startup?)
