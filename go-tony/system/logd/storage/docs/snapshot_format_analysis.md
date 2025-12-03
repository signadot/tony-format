# Snapshot Format Analysis: Tony Document vs Alternatives

## The Question

Should snapshots be stored as **Tony documents** (full state in the same format as the virtual document), or should they use a different format?

## Current Assumption

Snapshots are stored as `LogEntry` with `Diff` field containing **full state** (Tony document format).

## Option A: Snapshot as Tony Document (Full State)

**Format**: Snapshot is a complete Tony document representing the full state at a commit boundary.

**Structure**:
```go
type LogEntry struct {
    Commit    int64
    Seq       int64
    Timestamp string
    Diff      *ir.Node  // For snapshots: full state (Tony document)
}

// Snapshot at commit 16:
// Diff = {
//   "a": {
//     "b": {
//       "c": 42,
//       "d": "hello"
//     }
//   },
//   "x": [1, 2, 3]
// }
```

**Read Process**:
1. Read snapshot `LogEntry`
2. `entry.Diff` contains full state (Tony document)
3. Use directly as base state
4. Apply remaining diffs on top

### Pros

1. **Direct Usability**
   - Snapshot is immediately usable (full state)
   - No transformation needed
   - Can be read/used directly by clients

2. **Consistency**
   - Same format as virtual document
   - Same format as what clients read
   - No format conversion needed

3. **Simple Read Logic**
   - Read snapshot → use as base state
   - Apply remaining diffs
   - No special handling for snapshot format

4. **Efficient for Large Reads**
   - Reading full document = reading snapshot directly
   - No diff application needed if reading at snapshot boundary
   - Optimal for reading entire document state

5. **Natural Representation**
   - Snapshot represents "state at commit N"
   - Tony document represents "current state"
   - Natural mapping: snapshot = Tony document

6. **Reusability**
   - Snapshot can be used by other systems
   - Can be exported/imported as Tony document
   - Can be used for backups/restores

7. **Debugging**
   - Can inspect snapshot files directly
   - Can use Tony tools to view snapshots
   - Easier to debug state at boundaries

### Cons

1. **Storage Size**
   - Full state might be large
   - Even if only small changes, snapshot stores everything
   - Example: 1MB document, snapshot = 1MB (vs. small diff)

2. **Write Cost**
   - Must compute full state from all diffs
   - Must serialize entire document
   - More expensive than writing a diff

3. **No Incremental Updates**
   - Can't update snapshot incrementally
   - Must recompute from scratch each time
   - More work during compaction

4. **Redundancy**
   - Snapshot contains all data that's already in diffs
   - Some redundancy (but intentional for read optimization)

5. **Compression**
   - Full state might compress less well than diffs
   - But: Can compress snapshots independently

## Option B: Snapshot as Composed Diff

**Format**: Snapshot is a single diff that represents all changes from empty state.

**Structure**:
```go
type LogEntry struct {
    Commit    int64
    Seq       int64
    Timestamp string
    Diff      *ir.Node  // For snapshots: composed diff (all changes from empty)
}

// Snapshot at commit 16:
// Diff = {
//   "a": { "!insert": { "b": { "!insert": { "c": 42 } } } },
//   "x": { "!insert": [1, 2, 3] }
// }
```

**Read Process**:
1. Read snapshot `LogEntry`
2. `entry.Diff` contains composed diff
3. Apply diff to empty state → get full state
4. Apply remaining diffs on top

### Pros

1. **Smaller Storage**
   - Composed diff might be smaller than full state
   - Only stores changes, not entire structure
   - Better compression potential

2. **Consistent Format**
   - Both diffs and snapshots use diff format
   - Same interpretation logic
   - Simpler code (one format)

3. **Incremental Composition**
   - Can compose diffs incrementally
   - Might be more efficient to compute

### Cons

1. **Read Overhead**
   - Must apply diff to empty state first
   - Extra step in read process
   - More complex read logic

2. **Not Directly Usable**
   - Can't use snapshot directly
   - Must transform to full state
   - Extra computation on read

3. **Less Efficient for Full Reads**
   - Reading full document requires diff application
   - Even at snapshot boundary, must apply diff
   - Not optimal for common case

4. **Semantic Confusion**
   - "Snapshot" suggests full state, not a diff
   - Confusing to have snapshot be a diff
   - Less intuitive

5. **Composition Complexity**
   - Must compose all diffs correctly
   - More complex compaction logic
   - Potential for errors

## Option C: Snapshot as Special Format

**Format**: Snapshot uses a different format optimized for full state.

**Structure**:
```go
type SnapshotEntry struct {
    Commit    int64
    Timestamp string
    State     *ir.Node  // Full state in optimized format
}

// Separate from LogEntry
```

### Pros

1. **Optimized Format**
   - Can optimize format for full state
   - Might be more compact
   - Can use different encoding

2. **Type Safety**
   - Clear distinction: SnapshotEntry vs LogEntry
   - Can't accidentally mix formats
   - Compile-time safety

3. **Format Evolution**
   - Can evolve snapshot format independently
   - Can optimize without affecting diff format

### Cons

1. **Code Duplication**
   - Separate read/write code
   - Separate serialization
   - More code to maintain

2. **Complexity**
   - Two formats to handle
   - More complex read logic
   - More test code

3. **Inconsistency**
   - Different format than virtual document
   - Must convert between formats
   - More transformation logic

4. **Less Reusable**
   - Can't use snapshot directly
   - Must convert to Tony document
   - Less useful for other systems

## Comparison Table

| Aspect | Tony Document | Composed Diff | Special Format |
|--------|---------------|---------------|----------------|
| **Storage Size** | ❌ Large (full state) | ✅ Smaller (composed diff) | ⚠️ Depends on optimization |
| **Read Performance** | ✅ Fast (direct use) | ❌ Slower (apply diff) | ⚠️ Depends on format |
| **Write Cost** | ❌ High (compute full state) | ⚠️ Medium (compose diffs) | ⚠️ Depends on format |
| **Simplicity** | ✅ Simple (direct use) | ⚠️ Medium (apply diff) | ❌ Complex (two formats) |
| **Consistency** | ✅ Same as virtual doc | ⚠️ Same as diffs | ❌ Different format |
| **Reusability** | ✅ High (can export) | ❌ Low (must apply) | ⚠️ Medium (must convert) |
| **Debugging** | ✅ Easy (inspect directly) | ⚠️ Medium (must apply) | ⚠️ Medium (must convert) |
| **Code Complexity** | ✅ Low (one format) | ✅ Low (one format) | ❌ High (two formats) |

## Key Questions

1. **What's the primary use case?**
   - Reading full document? → Tony document wins
   - Reading incremental changes? → Either works
   - Exporting/backup? → Tony document wins

2. **What's the typical document size?**
   - Small (< 10KB): Either format is fine
   - Large (> 1MB): Storage size matters more
   - Very large (> 100MB): Compression matters

3. **What's the read pattern?**
   - Mostly full reads? → Tony document wins
   - Mostly incremental reads? → Either works
   - Mix? → Tony document is better (handles both)

4. **What's the write frequency?**
   - High frequency: Write cost matters (composed diff might be better)
   - Low frequency: Write cost less important

5. **Do we need to export snapshots?**
   - Yes: Tony document is better (directly usable)
   - No: Either format works

## Analysis: Tony Document vs Composed Diff

### Storage Size

**Tony Document**:
- Stores full state: O(document_size)
- Example: 1MB document → 1MB snapshot

**Composed Diff**:
- Stores changes only: O(changes_size)
- Example: 1MB document, 10KB changes → 10KB snapshot
- But: Must store entire structure if everything changed

**Verdict**: Composed diff can be smaller, but not always (if many changes, might be similar size).

### Read Performance

**Tony Document**:
- Read snapshot → use directly
- No transformation needed
- Optimal for full reads

**Composed Diff**:
- Read snapshot → apply to empty state → use
- Extra step (apply diff)
- Slower for full reads

**Verdict**: Tony document is faster for reads (common case).

### Write Cost

**Tony Document**:
- Must read all diffs
- Apply all diffs to compute full state
- Serialize full state
- O(N) where N = number of diffs

**Composed Diff**:
- Must compose all diffs
- Serialize composed diff
- O(N) where N = number of diffs
- But: Composed diff might be smaller to serialize

**Verdict**: Similar cost, but composed diff might be slightly cheaper (smaller serialization).

### Simplicity

**Tony Document**:
- Snapshot = full state
- Read: use directly
- Simple and intuitive

**Composed Diff**:
- Snapshot = diff (but represents full state)
- Read: apply diff first
- Less intuitive

**Verdict**: Tony document is simpler and more intuitive.

## Recommendation

**Recommendation: Snapshot as Tony Document (Full State)**

**Rationale**:
1. **Read performance**: Most important - snapshots optimize reads, Tony document is fastest
2. **Simplicity**: Direct use, no transformation needed
3. **Consistency**: Same format as virtual document
4. **Reusability**: Can export/use directly
5. **Debugging**: Easier to inspect and debug

**Trade-offs**:
- Storage size: Larger, but acceptable (snapshots are optimization, not primary storage)
- Write cost: Higher, but acceptable (compaction is background process)

**Implementation**:
```go
type LogEntry struct {
    Commit    int64
    Seq       int64
    Timestamp string
    Diff      *ir.Node  // For diffs: incremental changes
                         // For snapshots: full state (Tony document)
}

// Snapshot at commit 16:
// entry.Diff = full state as Tony document
// {
//   "a": {
//     "b": {
//       "c": 42,
//       "d": "hello"
//     }
//   },
//   "x": [1, 2, 3]
// }
```

**Read Logic**:
```go
// If IsSnapshot:
if segment.IsSnapshot {
    // entry.Diff contains full state (Tony document)
    state = entry.Diff  // Use directly
} else {
    // entry.Diff contains incremental changes
    state = applyDiff(state, entry.Diff)
}
```

## Conclusion

**Snapshot as Tony document (full state)** is recommended because:
- Optimal read performance (primary benefit of snapshots)
- Simple and intuitive
- Consistent with virtual document format
- Reusable and debuggable

Storage size and write cost are acceptable trade-offs for the read performance benefit.
