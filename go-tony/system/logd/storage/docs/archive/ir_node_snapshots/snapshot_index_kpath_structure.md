# Snapshot Index Structure: KPath String + Delta Offsets

## Proposed Structure

**Index Format**:
```
[Snapshot Size: varuint]
[Path Count: varuint]
For each path (pre-order DFS order):
  [Path String Length: varuint]
  [Path String: bytes]  // KPath.String() representation
  [Offset Delta: varuint]  // delta from previous offset
```

## Example

Snapshot with paths:
```
"" → offset 0
"a" → offset 100
"a.b" → offset 150
"a.c" → offset 200
"x" → offset 300
"x.y" → offset 350
```

**Index Encoding**:
```
Snapshot Size: 400
Path Count: 6
  Path: "" (empty string, length 0)
  Offset Delta: 0
  Path: "a" (length 1)
  Offset Delta: 100
  Path: "a.b" (length 3)
  Offset Delta: 50
  Path: "a.c" (length 3)
  Offset Delta: 50
  Path: "x" (length 1)
  Offset Delta: 100
  Path: "x.y" (length 3)
  Offset Delta: 50
```

## Lookup Operations

### Lookup by Path String

**Binary Search** (paths are in pre-order DFS order, which is lexicographically sorted):
```go
func (idx *SnapshotIndex) FindPath(pathString string) (offset int64, size int64, err error) {
    // Binary search for path string
    i := sort.Search(len(idx.paths), func(i int) bool {
        return idx.paths[i].path >= pathString
    })
    
    if i >= len(idx.paths) || idx.paths[i].path != pathString {
        return 0, 0, ErrPathNotFound
    }
    
    // Reconstruct absolute offset
    offset = idx.reconstructOffset(i)
    
    // Compute size
    if i < len(idx.paths)-1 {
        nextOffset := idx.reconstructOffset(i+1)
        size = nextOffset - offset
    } else {
        size = idx.snapshotSize - offset
    }
    
    return offset, size, nil
}
```

### Reconstruct Absolute Offsets

```go
func (idx *SnapshotIndex) reconstructOffset(i int) int64 {
    offset := int64(0)
    for j := 0; j <= i; j++ {
        offset += idx.paths[j].offsetDelta
    }
    return offset
}
```

**Optimization**: Cache reconstructed offsets if needed.

## Benefits

### ✅ **Sane and Reasonable**

1. **Explicit Paths**: Path strings are human-readable and debuggable
2. **Standard Format**: Uses existing `KPath.String()` representation
3. **Efficient Lookup**: Binary search on sorted path strings
4. **Compact**: Delta encoding reduces offset storage
5. **Complete**: Can compute sizes from offsets + snapshot size

### ✅ **Pre-order DFS Order Benefits**

1. **Lexicographic Sort**: Pre-order DFS naturally sorts paths lexicographically
   - Enables efficient binary search
   - Matches Tony format structure
2. **Parent-Child Ordering**: Parents always come before children
   - Useful for hierarchical queries
   - Can efficiently find all descendants of a path

### ✅ **KPath.String() Benefits**

1. **Canonical**: Standard representation already used throughout codebase
2. **Parseable**: Can convert back to `KPath` via `ParseKPath()`
3. **Quoted Fields**: Handles special characters correctly
4. **All Accessors**: Supports fields, arrays, sparse arrays, wildcards

## Storage Size

**Path String Storage**:
- Average path length: ~10-20 bytes (depends on depth)
- For 10,000 paths: ~100-200 KB for path strings
- For 1,000,000 paths: ~10-20 MB for path strings

**Offset Delta Storage**:
- Delta encoding: typically 1-2 bytes per offset (varuint)
- For 10,000 paths: ~10-20 KB
- For 1,000,000 paths: ~1-2 MB

**Total Index Size** (for 1M paths):
- Path strings: ~10-20 MB
- Offsets: ~1-2 MB
- **Total: ~11-22 MB** (reasonable for large snapshots)

## Alternative: Compressed Path Strings

**Option**: Store path segments separately to reduce redundancy:
```
[Snapshot Size: varuint]
[Path Count: varuint]
For each path (pre-order DFS order):
  [Segment Count: varuint]
  For each segment:
    [Segment Type: byte]  // field, index, sparse index
    [Segment Value: encoded]
  [Offset Delta: varuint]
```

**Benefits**: More compact (shared prefixes)
**Costs**: More complex encoding/decoding

**Recommendation**: Start with `KPath.String()` - simple and sufficient.

## Implementation

### Index Structure

```go
type SnapshotPathIndex struct {
    SnapshotSize int64
    Paths        []PathIndexEntry
}

type PathIndexEntry struct {
    Path       string  // KPath.String() representation
    OffsetDelta uint64 // Delta from previous offset
}

// Reconstructed for lookup (computed on-demand or cached)
type PathIndexEntryWithOffset struct {
    Path   string
    Offset int64
    Size   int64
}
```

### Building Index

```go
func BuildSnapshotIndex(snapshot *ir.Node, snapshotSize int64) (*SnapshotPathIndex, error) {
    index := &SnapshotPathIndex{
        SnapshotSize: snapshotSize,
        Paths:        []PathIndexEntry{},
    }
    
    var lastOffset int64 = 0
    
    // Traverse snapshot in pre-order DFS
    traversePreOrderDFS(snapshot, "", func(path string, node *ir.Node, offset int64) {
        pathString := path // or convert to KPath and call String()
        delta := offset - lastOffset
        
        index.Paths = append(index.Paths, PathIndexEntry{
            Path:       pathString,
            OffsetDelta: uint64(delta),
        })
        
        lastOffset = offset
    })
    
    return index, nil
}
```

### Reading Index

```go
func ReadSnapshotIndex(reader io.Reader) (*SnapshotPathIndex, error) {
    // Read snapshot size
    snapshotSize, err := binary.ReadUvarint(reader)
    if err != nil {
        return nil, err
    }
    
    // Read path count
    pathCount, err := binary.ReadUvarint(reader)
    if err != nil {
        return nil, err
    }
    
    index := &SnapshotPathIndex{
        SnapshotSize: int64(snapshotSize),
        Paths:        make([]PathIndexEntry, 0, pathCount),
    }
    
    // Read each path entry
    for i := 0; i < int(pathCount); i++ {
        // Read path string length
        pathLen, err := binary.ReadUvarint(reader)
        if err != nil {
            return nil, err
        }
        
        // Read path string
        pathBytes := make([]byte, pathLen)
        if _, err := io.ReadFull(reader, pathBytes); err != nil {
            return nil, err
        }
        
        // Read offset delta
        offsetDelta, err := binary.ReadUvarint(reader)
        if err != nil {
            return nil, err
        }
        
        index.Paths = append(index.Paths, PathIndexEntry{
            Path:       string(pathBytes),
            OffsetDelta: offsetDelta,
        })
    }
    
    return index, nil
}
```

## Conclusion

**Yes, using `KPath.String()` with delta-encoded offsets is a reasonable and sane index structure:**

✅ **Simple**: Uses existing string representation
✅ **Efficient**: Binary search on sorted paths
✅ **Complete**: Can compute sizes from offsets
✅ **Debuggable**: Human-readable path strings
✅ **Standard**: Uses existing `KPath` infrastructure

**Recommendation**: ✅ **Use this structure**
