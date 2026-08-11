# Smaller Gaps to Fill (Before Streaming Diff Application)

## 1. Compaction Boundary Detection ⚠️

**Status**: Not implemented

**What's Needed**:
```go
// Check if a commit is at a compaction boundary
func IsCompactionBoundary(commit int64, divisor int) bool {
    // Check if commit is at exponential boundary: 16, 32, 64, 128, ...
    // Formula: commit % (divisor^boundary) == 0
}
```

**Where It's Used**:
- Before calling `SwitchActive()` - need to detect when boundary is reached
- In `commit_ops.go` or `storage.go` - check after each commit
- Triggers compaction workflow

**Complexity**: Low - Simple math check

---

## 2. Reading from SnapPos (Raw ir.Node) ⚠️

**Status**: Not implemented

**What Exists**:
- ✅ `ReadEntryAt()` - Reads `dlog.Entry` (wrapped structure)
- ✅ `Entry.SnapPos` - Points to on-disk `ir.Node` position

**What's Missing**:
```go
// Read raw ir.Node from log position (for SnapPos)
func (dlf *DLogFile) ReadNodeAt(position int64) (*ir.Node, error) {
    // Read length prefix
    // Create section reader
    // Use TokenSource + NodeParser to parse ir.Node
    // Return parsed node (not wrapped in Entry)
}
```

**Why Needed**:
- `SnapPos` points to raw `ir.Node` data, not an Entry
- Need to read snapshot data for streaming diff application
- Need to read snapshot data for ReadStateAt

**Complexity**: Low-Medium - Uses existing TokenSource + NodeParser

---

## 3. Snapshot Entry Creation API ⚠️

**Status**: Not implemented

**What's Needed**:
```go
// Create snapshot entry pointing to snapshot data
func CreateSnapshotEntry(
    commit int64,
    timestamp string,
    snapshotPosition int64,  // Where snapshot data was written
) *dlog.Entry {
    return &dlog.Entry{
        Commit:    commit,
        Timestamp: timestamp,
        SnapPos:   &snapshotPosition,
        Patch:     nil,
        TxSource:  nil,
        LastCommit: nil,
    }
}

// Write snapshot data and create snapshot entry
func (dl *DLog) WriteSnapshot(
    commit int64,
    timestamp string,
    snapshotData *ir.Node,  // Full state
) (snapshotPos int64, entry *dlog.Entry, error) {
    // 1. Write snapshot data to inactive log (get position)
    // 2. Create snapshot entry pointing to that position
    // 3. Return both
}
```

**Why Needed**:
- Need clear API for creating snapshot entries
- Need to write snapshot data first, then create Entry pointing to it
- Used during compaction

**Complexity**: Low-Medium - Combines writing + entry creation

---

## 4. Basic In-Memory Diff Application ⚠️

**Status**: Not implemented

**What Exists**:
- ✅ `tx.MergePatches()` - Merges multiple patches (doesn't apply to state)

**What's Needed**:
```go
// Apply diff to existing state (in-memory)
func ApplyDiff(baseState *ir.Node, diff *ir.Node) (*ir.Node, error) {
    // Merge diff into baseState
    // Handle:
    // - Object field updates/insertions
    // - Array element updates (with !arraydiff tag)
    // - Sparse array updates
    // - Deletions (null values?)
    // - Nested updates
    // Returns new merged state
}
```

**Why Needed**:
- Useful for small cases (patches are small)
- Useful for testing/validation
- Can use for ReadStateAt when state fits in memory
- Simpler than streaming, can validate streaming implementation

**Complexity**: Medium - Need to handle all node types and merge semantics

---

## 5. Index Updates for Snapshots ⚠️

**Status**: Partially implemented

**What Exists**:
- ✅ `IndexPatch()` - Indexes patch entries
- ✅ `NewLogSegmentFromPatchEntry()` - Creates LogSegment from patch

**What's Missing**:
```go
// Index snapshot entry
func IndexSnapshot(
    idx *Index,
    entry *dlog.Entry,  // Snapshot entry (SnapPos set)
    logFile string,
    pos int64,
    txSeq int64,
) error {
    // Create LogSegment for snapshot
    // Set IsSnapshot flag (or StartCommit == EndCommit)
    // Index at root path ("")
    // Maybe also index all paths? (depends on design)
}
```

**Why Needed**:
- Snapshots need to be indexed differently than patches
- Need to mark segments as snapshots
- Used during compaction

**Complexity**: Low - Similar to IndexPatch but for snapshots

---

## 6. Log Switching Integration ⚠️

**Status**: Partially implemented

**What Exists**:
- ✅ `SwitchActive()` - Switches active log (A ↔ B)
- ✅ `GetActiveLog()` / `GetInactiveLog()` - Get current active/inactive log

**What's Missing**:
- Integration with commit flow
- When to call `SwitchActive()` (at compaction boundaries)
- Coordination with snapshot creation

**What's Needed**:
```go
// In commit_ops.go or storage.go
func (c *commitOps) WriteAndIndex(...) {
    // ... write entry ...
    
    // Check if compaction boundary reached
    if IsCompactionBoundary(commit, divisor) {
        // Switch active log
        c.s.dLog.SwitchActive()
        
        // Trigger snapshot creation (async or sync?)
        // This is where compaction would happen
    }
}
```

**Complexity**: Low-Medium - Integration logic, need to decide sync vs async

---

## 7. IsSnapshot Flag Clarification ⚠️

**Status**: Design inconsistency

**What's Needed**:
- Either add `IsSnapshot bool` field to `LogSegment`
- Or clarify that `StartCommit == EndCommit` means snapshot
- Update design docs to match implementation

**Complexity**: Low - Just add field or update docs

---

## Summary: Priority Order

1. **Compaction Boundary Detection** (Low complexity, blocks log switching)
2. **Reading from SnapPos** (Low-Medium, needed for snapshot reads)
3. **Basic In-Memory Diff Application** (Medium, useful for testing/small cases)
4. **Snapshot Entry Creation API** (Low-Medium, needed for compaction)
5. **Index Updates for Snapshots** (Low, needed for compaction)
6. **Log Switching Integration** (Low-Medium, ties everything together)
7. **IsSnapshot Flag Clarification** (Low, documentation/consistency)

**Estimated Effort**:
- Items 1-2: 1-2 days
- Items 3-4: 2-3 days  
- Items 5-7: 1-2 days
- **Total**: ~1 week for all smaller gaps

**After These**: Ready to tackle streaming diff application (complex, but foundation will be solid)
