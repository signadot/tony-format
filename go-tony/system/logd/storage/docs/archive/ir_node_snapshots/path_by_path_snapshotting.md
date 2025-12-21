# Path-by-Path Snapshotting Approach

## Overview

Alternative approach to snapshotting that processes paths one at a time, applying patches per-path rather than streaming the entire snapshot through token mutation.

## Key Insight

Instead of:
- Stream entire snapshot → Apply patches via token mutation → Stream result

Do:
- Iterate paths in order → For each path: read path, apply patches, write path → Continue

## Components Needed

### 1. Path Iterator

**In-Memory Implementation**:
```go
type PathIterator interface {
    // Next returns the next path in depth-first pre-order traversal
    // Returns path, node at path, and done flag
    Next() (kindedPath string, node *ir.Node, done bool, err error)
}

// In-memory: Traverse ir.Node tree depth-first pre-order
func (n *ir.Node) PathIterator() PathIterator {
    return &inMemoryPathIterator{node: n, currentPath: ""}
}
```

**Streaming Implementation** (using token.Source):
```go
// Use TokenSource to track paths as tokens are read
// TokenSink already tracks paths via updatePath()
// Could create TokenPathIterator that reads tokens and emits paths
func NewTokenPathIterator(source *token.TokenSource) PathIterator {
    return &tokenPathIterator{source: source}
}
```

### 2. Path Offset Index (for Snapshots)

**Current State**: Index has `LogPosition` for patches, but not path offsets within snapshots.

**Needed**: Index that maps `(snapshotPosition, kindedPath) → byteOffset`

```go
type SnapshotPathIndex interface {
    // GetPathOffset returns the byte offset of a path within a snapshot
    GetPathOffset(snapshotLogFile LogFileID, snapshotPosition int64, kindedPath string) (byteOffset int64, err error)
    
    // ListPaths returns all paths in a snapshot in depth-first pre-order
    ListPaths(snapshotLogFile LogFileID, snapshotPosition int64) ([]string, error)
}
```

**When Built**: During snapshot creation (compaction)
- As snapshot is written, track path boundaries and offsets
- Store in index for later use

### 3. Per-Path Patch Applier

```go
type PerPathPatchApplier interface {
    // ApplyPatchesToPath applies patches affecting a specific path
    // Parameters:
    //   - basePathNode: The node at this path from base snapshot (nil if doesn't exist)
    //   - patches: List of patches affecting this path (from index lookup)
    //
    // Returns the patched node for this path
    ApplyPatchesToPath(basePathNode *ir.Node, patches []*ir.Node) (*ir.Node, error)
}
```

**Implementation**:
- In-memory: Apply patch to base path node (need to implement patch application logic)
- Patches are small (fit in memory)
- Path nodes are small (fit in memory)
- Note: `tx.MergePatches()` merges multiple patches into one root diff, but doesn't apply patches to base state

## Workflow

### Path-by-Path Snapshotting

```go
func CreateSnapshotPathByPath(
    baseSnapshot SnapshotReader,
    patches []*ir.Node,
    resultWriter SnapshotWriter,
    pathIndex SnapshotPathIndex, // Optional - for streaming snapshot reads
) error {
    // 1. Get paths in order
    var pathIterator PathIterator
    if baseSnapshot != nil {
        // In-memory: traverse tree
        // Streaming: use token.Source to track paths
        pathIterator = baseSnapshot.PathIterator()
    } else {
        // Empty snapshot - get paths from patches
        pathIterator = getPathsFromPatches(patches)
    }
    
    // 2. For each path
    for {
        path, baseNode, done, err := pathIterator.Next()
        if done {
            break
        }
        if err != nil {
            return err
        }
        
        // 3. Get patches affecting this path (from index)
        pathPatches := getPatchesForPath(path, patches)
        
        // 4. Apply patches to this path
        patchedNode, err := applyPatchesToPath(baseNode, pathPatches)
        if err != nil {
            return err
        }
        
        // 5. Write patched path to result
        if err := resultWriter.WritePath(path, patchedNode); err != nil {
            return err
        }
    }
    
    return nil
}
```

## Comparison: Token Mutation vs Path-by-Path

### Token Mutation Approach (Current Design)
**Pros**:
- ✅ Single pass through snapshot
- ✅ No need for path indexing
- ✅ Works with existing TokenSink infrastructure

**Cons**:
- ⚠️ Requires token mutation callbacks (complex)
- ⚠️ Need to match paths between snapshot and patches while streaming
- ⚠️ Harder to debug/test

### Path-by-Path Approach (Alternative)
**Pros**:
- ✅ Simpler: Each path processed independently
- ✅ Patches applied in-memory (simple `ir.Merge()`)
- ✅ Easier to debug/test (one path at a time)
- ✅ Can leverage path indexing for efficient reads

**Cons**:
- ⚠️ Requires path indexing for snapshots (not yet built)
- ⚠️ Multiple passes (one per path) vs single pass
- ⚠️ Need path iterator (from token.Source or tree traversal)

## Questions to Answer

1. **Path Iterator from token.Source**:
   - Can we get paths in order as tokens are read?
   - TokenSink already tracks paths - can we use similar logic?
   - Or do we need to parse tokens into paths?

2. **Snapshot Path Index**:
   - When/how is it built? (During snapshot creation)
   - Where is it stored? (In index, separate file?)
   - How do we get offsets? (Track during snapshot write)

3. **Path Ordering**:
   - Depth-first pre-order is natural for tree traversal
   - But is this the order paths appear in Tony format?
   - Token.Source reads tokens in order - paths should match

4. **Hybrid Approach**:
   - Use path iterator to get paths in order
   - For each path, read from snapshot (using offset if available)
   - Apply patches in-memory
   - Write patched path
   - Continue to next path

## Implementation Feasibility

### In-Memory Implementation ✅
- Path iterator: Tree traversal (depth-first pre-order)
- Read path: Extract from `ir.Node` via `GetKPath()`
- Apply patches: Apply patch to base path node (need implementation)
- Write path: Encode and append to result

### Streaming Implementation ⚠️
- Path iterator: Use `token.Source` + path tracking (like TokenSink)
- Read path: Use path index to find offset, read sub-tree
- Apply patches: In-memory (patches are small)
- Write path: Stream to result writer

**Challenge**: Need snapshot path index for efficient path reads.

## Recommendation

**Path-by-path approach is viable** if:
1. We can build snapshot path index during snapshot creation
2. We can iterate paths in order (from token.Source or tree)
3. Per-path processing is efficient enough

**Advantages**:
- Simpler than token mutation
- Easier to test/debug
- Each path processed independently (small, fits in memory)

**Consider**: Hybrid approach - use path iterator, but apply patches per-path rather than streaming mutation.
