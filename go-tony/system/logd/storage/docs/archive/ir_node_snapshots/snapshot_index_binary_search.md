# Binary Search with Delta-Encoded Offsets

## The Question

Can we do binary search efficiently when offsets are delta-encoded?

## Answer: Yes, but...

**Binary search on path strings works fine** - we're comparing path strings, not offsets.

**However**, if we need the offset after finding the path, we need to reconstruct it from deltas.

## Binary Search Process

### Step 1: Search by Path String (No Offset Needed)

```go
func (idx *SnapshotIndex) FindPath(pathString string) (int, error) {
    // Binary search on path strings - offsets not needed here
    i := sort.Search(len(idx.paths), func(i int) bool {
        return idx.paths[i].path >= pathString  // Compare strings only
    })
    
    if i >= len(idx.paths) || idx.paths[i].path != pathString {
        return -1, ErrPathNotFound
    }
    
    return i, nil  // Found index
}
```

**This works fine** - we're only comparing path strings, deltas don't matter.

### Step 2: Reconstruct Offset (If Needed)

```go
func (idx *SnapshotIndex) GetOffset(index int) int64 {
    // Need to sum all deltas up to this index
    offset := int64(0)
    for j := 0; j <= index; j++ {
        offset += int64(idx.paths[j].offsetDelta)
    }
    return offset
}
```

**This is O(n) in worst case** - if we need offset for every lookup, we might sum many deltas.

## The Problem

If we need offsets frequently, reconstructing from deltas can be expensive:
- **Worst case**: O(n) to reconstruct offset for last path
- **Average case**: O(n/2) for random lookups

## Solutions

### Option 1: Cache Reconstructed Offsets

**Build offset array when loading index**:
```go
type SnapshotIndex struct {
    SnapshotSize int64
    Paths        []PathIndexEntry
    Offsets      []int64  // Cached absolute offsets
}

func ReadSnapshotIndex(reader io.Reader) (*SnapshotIndex, error) {
    // ... read paths and deltas ...
    
    // Reconstruct all offsets once
    index.Offsets = make([]int64, len(index.Paths))
    offset := int64(0)
    for i := 0; i < len(index.Paths); i++ {
        offset += int64(index.Paths[i].offsetDelta)
        index.Offsets[i] = offset
    }
    
    return index, nil
}
```

**Lookup**:
```go
func (idx *SnapshotIndex) FindPath(pathString string) (offset int64, size int64, err error) {
    // Binary search (O(log n))
    i := sort.Search(len(idx.paths), func(i int) bool {
        return idx.paths[i].path >= pathString
    })
    
    if i >= len(idx.paths) || idx.paths[i].path != pathString {
        return 0, 0, ErrPathNotFound
    }
    
    // Get cached offset (O(1))
    offset = idx.Offsets[i]
    
    // Compute size (O(1))
    if i < len(idx.paths)-1 {
        size = idx.Offsets[i+1] - offset
    } else {
        size = idx.snapshotSize - offset
    }
    
    return offset, size, nil
}
```

**Trade-off**: 
- ✅ O(log n) lookup + O(1) offset access
- ⚠️ Extra memory: ~8 bytes per path (for 1M paths: ~8 MB)

### Option 2: Store Absolute Offsets (No Delta Encoding)

**Simpler but larger**:
```go
type PathIndexEntry struct {
    Path   string
    Offset int64  // Absolute offset, not delta
}
```

**Trade-off**:
- ✅ O(log n) lookup, O(1) offset access, no reconstruction needed
- ⚠️ Larger storage: ~8 bytes per offset (vs ~1-2 bytes for deltas)

### Option 3: Sparse Caching

**Cache offsets at regular intervals**:
```go
type SnapshotIndex struct {
    Paths        []PathIndexEntry
    OffsetCache  []int  // Indices where we cache offsets
    CachedOffsets []int64
}

// Cache every 1000th offset
func (idx *SnapshotIndex) GetOffset(index int) int64 {
    // Find nearest cached offset
    cacheIdx := index / 1000
    offset := idx.CachedOffsets[cacheIdx]
    
    // Sum deltas from cache point to target
    for j := cacheIdx * 1000; j < index; j++ {
        offset += int64(idx.paths[j].offsetDelta)
    }
    
    return offset
}
```

**Trade-off**:
- ✅ Reduced memory (cache every Nth offset)
- ⚠️ Still need to sum some deltas (but fewer)

## Recommendation

**Option 1: Cache Reconstructed Offsets** seems best:

1. **Delta encoding saves space** during storage/write
2. **Cached offsets enable fast lookup** during read
3. **Memory cost is reasonable**: ~8 MB for 1M paths
4. **Simple implementation**: Reconstruct once when loading index

**Storage Format** (delta-encoded):
```
[Snapshot Size: varuint]
[Path Count: varuint]
For each path:
  [Path String: varuint length + bytes]
  [Offset Delta: varuint]
```

**In-Memory Structure** (cached offsets):
```go
type SnapshotIndex struct {
    SnapshotSize int64
    Paths        []PathIndexEntry  // path + offsetDelta
    Offsets      []int64           // cached absolute offsets
}
```

## Conclusion

**Binary search works fine with delta encoding** - we search by path strings, not offsets.

**But** if we need offsets frequently, cache them when loading the index. The delta encoding saves space during storage, and cached offsets enable fast lookups.
