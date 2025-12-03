# Read Algorithm: Detailed Specification

> **Design Reference**: See `DESIGN.md` for the high-level read flow overview.

## Overview

This document specifies the detailed algorithm for reading state at a specific kinded path and commit number. This is the authoritative specification for the `ReadAt` operation.

**⚠️ Dependency Note**: This algorithm references compaction and snapshots, which are not yet fully designed. See `compaction_snapshot_design.md` for the dependency cycle and design questions. The compaction-related parts of this algorithm are **provisional** and subject to change based on the final compaction design.

## Function Signature

```go
func (s *Storage) ReadAt(kindedPath string, commit int64) (*ir.Node, error)
```

**Parameters**:
- `kindedPath`: Kinded path to read (e.g., `"a.b.c"`, `""` for root)
- `commit`: Commit number to read at (must be >= 0)

**Returns**:
- `*ir.Node`: The state at the specified path and commit
- `error`: Error if read fails

## Algorithm

### Step 1: Get Index Iterator at Path

Get an iterator positioned at `kindedPath` in the index hierarchy.

```go
index.RLock()
defer index.RUnlock()

it := index.IterAtPath(kindedPath)
if !it.Valid() {
    // Path doesn't exist in index - return empty state
    return nil, nil
}
```

**Note**: The iterator is positioned at the index node for `kindedPath`. If the path doesn't exist, the iterator is invalid and we return empty state.

### Step 2: Iterate Commits in Descending Order

Use the iterator to iterate commits in descending order starting from `commit`, stopping when we've read enough to reconstruct state.

```go
var result *ir.Node

// Iterate commits starting from the requested commit, going downward
for seg := range it.CommitsAt(commit, Down) {
    // CommitsAt already filters to segments <= commit, so we can process all
    
    // Skip segments covered by compacted segment (handled in Step 5)
    // ... (see Step 5)
    
    // Read entry and apply diff
    entry, err := readEntryAt(logFile, seg.LogPosition)
    if err != nil {
        return nil, fmt.Errorf("failed to read entry at %d: %w", seg.LogPosition, err)
    }
    
    // Extract diff for this path from root diff
    diff, err := kindedpath.Get(entry.Diff, seg.KindedPath)
    if err != nil {
        // Path not found in diff - skip (shouldn't happen if index is correct)
        continue
    }
    
    // Apply diff to result
    result = applyDiff(result, diff)
    
    // Early termination: if all remaining segments are covered by compacted segment
    // (handled in Step 5)
}
```

**Key Optimization**: `CommitsAt(commit, Down)` seeks to the first segment <= `commit` and iterates downward. We only read segments up to the requested commit, stopping early when all necessary history is read.

### Step 3: Determine Starting Level

Determine the highest compaction level that covers `commit` using alignment rules.

**Alignment Rules**:
- Level 0: Always correct at any commit N
- Level 1: Correct at commit N if `N % divisor == 0`
- Level 2: Correct at commit N if `N % divisor^2 == 0`
- Level L: Correct at commit N if `N % divisor^L == 0`

**Algorithm**:
```go
func determineStartingLevel(commit int64, divisor int64) int {
    level := 0
    for {
        // Check if level L is correct at commit
        // Level L is correct if commit % divisor^L == 0
        divisorPower := int64(1)
        for i := 0; i < level; i++ {
            divisorPower *= divisor
        }
        if commit%divisorPower != 0 {
            break
        }
        level++
        // Safety: prevent infinite loop
        if level > 100 {
            break
        }
    }
    return level - 1 // Return highest level that is correct
}
```

**Example**:
- `commit = 16`, `divisor = 2`
- Level 0: `16 % 1 == 0` ✓
- Level 1: `16 % 2 == 0` ✓
- Level 2: `16 % 4 == 0` ✓
- Level 3: `16 % 8 == 0` ✓
- Level 4: `16 % 16 == 0` ✓
- Level 5: `16 % 32 != 0` ✗
- **Result**: Starting level = 4

### Step 4: Read Snapshot/Compacted Segment (if starting level > 0)

**⚠️ Provisional**: This step depends on compaction design which is not yet finalized.

If `startingLevel > 0`, read the snapshot/compacted segment for `kindedPath` at that level.

**Algorithm**:
```go
if startingLevel > 0 {
    snapshot := readSnapshot(kindedPath, startingLevel, commit)
    if snapshot != nil {
        // Use snapshot as base state
        result = snapshot.State
        // Skip segments covered by snapshot during iteration
    }
}
```

**Snapshot/Compacted Segment** (provisional assumptions):
- Contains full state at `commit` aligned to `startingLevel`
- Covers commit range `[snapshotStartCommit, commit]`
- All segments with commits in this range are covered
- **Note**: Compaction design may change this - snapshots may be separate from compaction, or compaction may produce merged diffs instead of full state

**Filtering Segments**:
```go
func filterSegmentsCoveredByCompacted(segments []*LogSegment, compacted *CompactedSegment) []*LogSegment {
    filtered := []*LogSegment{}
    for _, seg := range segments {
        // Keep segments that are NOT covered by compacted segment
        if seg.EndCommit < compacted.StartCommit || seg.StartCommit > compacted.EndCommit {
            filtered = append(filtered, seg)
        }
    }
    return filtered
}
```

### Step 5: Handle Compacted Segments (if starting level > 0)

If `startingLevel > 0` and we have a compacted segment, use it as base state and skip segments covered by it.

**Algorithm**:
```go
if startingLevel > 0 {
    compactedSegment := readCompactedSegment(kindedPath, startingLevel, commit)
    if compactedSegment != nil {
        // Use compacted segment as base state
        result = compactedSegment.State
        
        // Skip segments covered by compacted segment during iteration
        // We can do this by checking segment.EndCommit < compactedSegment.StartCommit
        // and breaking early in the iteration loop
    }
}
```

**Integration with Iterator**: When iterating commits, skip segments that are covered by the compacted segment:

```go
for seg := range it.Commits(Down) {
    // Skip segments covered by compacted segment
    if compactedSegment != nil && seg.EndCommit < compactedSegment.StartCommit {
        continue // Covered by compacted segment
    }
    
    // ... rest of iteration logic ...
}
```

### Step 6: Extract Final Path (if needed)

If `kindedPath != ""` and we read from root or ancestor segments, extract the final path.

**Algorithm**:
```go
if kindedPath != "" && result != nil {
    // Check if result is for a parent path
    // If we have segments with KindedPath != kindedPath, we need to extract
    finalDiff, err := kindedpath.Get(result, kindedPath)
    if err == nil {
        result = finalDiff
    }
    // If error, result is already at correct path (exact match)
}
```

**Note**: This step may not be needed if we only use segments with exact `KindedPath` match. But if we include ancestor segments, we need to extract.

### Step 7: Return Result

Return the final state.

```go
return result, nil
```

## Diff Application

### Function: `applyDiff`

Apply a diff to existing state, merging the changes.

**Signature**:
```go
func applyDiff(state *ir.Node, diff *ir.Node) *ir.Node
```

**Algorithm**:
1. If `state == nil`: return `diff` (first diff)
2. If `diff == nil`: return `state` (no change)
3. Merge `diff` into `state`:
   - For objects: merge fields
   - For arrays: merge elements
   - For deletions: remove fields/elements
   - For insertions: add fields/elements

**Implementation** (simplified):
```go
func applyDiff(state *ir.Node, diff *ir.Node) *ir.Node {
    if state == nil {
        return diff
    }
    if diff == nil {
        return state
    }
    
    // Merge diff into state
    // This is a simplified version - actual implementation depends on ir.Node structure
    return mergeNodes(state, diff)
}
```

**Note**: Actual implementation depends on `ir.Node` structure and diff format. This may use existing merge utilities from the Tony format library.

## Complete Algorithm (Pseudocode)

```go
func ReadAt(kindedPath string, commit int64) (*ir.Node, error) {
    // Step 1: Get index iterator at path
    index.RLock()
    defer index.RUnlock()
    
    it := index.IterAtPath(kindedPath)
    if !it.Valid() {
        // Path doesn't exist - return empty state
        return nil, nil
    }
    
    // Step 2: Determine starting level
    startingLevel := determineStartingLevel(commit, divisor)
    
    // Step 3: Read compacted segment (if any)
    var result *ir.Node
    var compactedSegment *CompactedSegment
    if startingLevel > 0 {
        compactedSegment = readCompactedSegment(kindedPath, startingLevel, commit)
        if compactedSegment != nil {
            result = compactedSegment.State
        }
    }
    
    // Step 4: Iterate commits starting from commit in descending order
    // CommitsAt seeks to commit and iterates downward, only yielding segments <= commit
    for seg := range it.CommitsAt(commit, Down) {
        // Skip segments covered by compacted segment
        if compactedSegment != nil && seg.EndCommit < compactedSegment.StartCommit {
            continue // Covered by compacted segment
        }
        
        // Read entry at LogPosition
        entry, err := readEntryAt(logFile, seg.LogPosition)
        if err != nil {
            return nil, fmt.Errorf("failed to read entry at %d: %w", seg.LogPosition, err)
        }
        
        // Extract diff for this path from root diff
        diff, err := kindedpath.Get(entry.Diff, seg.KindedPath)
        if err != nil {
            // Path not found in diff - skip (shouldn't happen if index is correct)
            continue
        }
        
        // Apply diff to result
        result = applyDiff(result, diff)
        
        // Early termination: if we've read all segments up to compacted segment
        if compactedSegment != nil && seg.EndCommit <= compactedSegment.StartCommit {
            break // All remaining segments are covered
        }
    }
    
    // Step 5: Extract final path (if needed)
    if kindedPath != "" && result != nil {
        finalDiff, err := kindedpath.Get(result, kindedPath)
        if err == nil {
            result = finalDiff
        }
    }
    
    // Step 6: Return result
    return result, nil
}
```

## Key Optimizations

### 1. Iterator-Based Iteration with Commit Seeking

**Before**: Query all segments, filter, sort, then iterate
```go
segments := index.LookupRange(kindedPath, nil, &commit) // Reads all segments
// Filter, sort, then iterate
```

**After**: Use iterator to seek to commit and iterate efficiently
```go
it := index.IterAtPath(kindedPath)
for seg := range it.CommitsAt(commit, Down) {
    // Process segments starting from commit, going downward
}
```

**Benefits**:
- No need to materialize all segments
- Seek directly to the target commit
- Iterate in commit order (descending) naturally
- Stop early when done
- More memory efficient
- More efficient I/O (only read necessary segments)

### 2. Early Termination

**Before**: Read all segments up to commit, then apply all

**After**: Stop when we've read enough
- Stop when segments are covered by compacted segment
- Stop when we've read past what we need (handled automatically by `CommitsAt`)

**Benefits**:
- Fewer reads
- Faster for recent commits
- Better performance overall


## Edge Cases

### 1. Empty State

**Scenario**: No segments found, no compacted segment
**Result**: Return `nil` (empty state)

### 2. Path Not Found

**Scenario**: Segments exist but path not found in diff
**Result**: Skip segment (shouldn't happen if index is correct), continue with next

### 3. Multiple Segments at Same Commit

**Scenario**: Multiple segments with same `StartCommit` and `StartTx`
**Result**: Apply in order (sorted by `KindedPath`), later segments override earlier ones

### 4. Corrupted Entry

**Scenario**: `readEntryAt` fails (corrupted entry)
**Result**: Return error, don't continue

### 5. Compaction Boundary

**Scenario**: `commit` is exactly at compaction boundary
**Result**: Use compacted segment, no additional segments needed

## Performance Considerations

### Optimization: Iterator-Based Iteration

**Key**: Use index iterators instead of querying all segments.

**Benefits**:
- No need to materialize all segments in memory
- Iterate in commit order naturally (descending)
- Stop early when done
- More memory efficient

### Optimization: Early Termination

Stop iteration when we've read enough:
- When segments are covered by compacted segment
- When we've read past the requested commit (handled automatically by `CommitsAt`)

### Optimization: Batch Reads (Future)

If multiple segments are in the same log file, batch reads for better I/O performance.

```go
// Group segments by log file and offset
segmentsByFile := groupSegmentsByFile(segments)
for file, fileSegments := range segmentsByFile {
    // Read all entries from this file in one pass
    entries := readEntriesBatch(file, fileSegments)
    // Apply entries
    for _, entry := range entries {
        result = applyDiff(result, entry.Diff)
    }
}
```

**Note**: This optimization is less critical with iterator-based iteration since we process segments one at a time and can stop early.

## Testing

### Test Cases

1. ✅ Read at root (`""`) with no segments → empty state
2. ✅ Read at root with one segment → returns diff
3. ✅ Read at nested path (`"a.b.c"`) → extracts path correctly
4. ✅ Read with multiple segments → applies in order
5. ✅ Read with compacted segment → uses compacted, filters segments
6. ✅ Read at compaction boundary → uses compacted segment
7. ✅ Read with ancestor segments → extracts correctly
8. ✅ Read with corrupted entry → returns error
9. ✅ Read with path not in diff → skips segment
10. ✅ Read with empty diff → no change to state

## Open Questions

1. **Segment Filtering**: Should we include ancestor/descendant segments, or only exact matches?
   - **Current**: Include ancestors (parent diffs include children)
   - **Alternative**: Only exact matches (simpler, but may miss parent changes)

2. **Diff Application**: How exactly does `applyDiff` work?
   - Need to understand `ir.Node` structure
   - Need to understand diff format (insert, delete, update operations)

3. **Compaction Integration**: How are compacted segments stored and read?
   - Need to design compaction format
   - Need to design compaction read API

4. **Error Handling**: What happens on partial failures?
   - Corrupted entry: return error or skip?
   - Missing log file: return error or empty state?
