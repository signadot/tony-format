# Snapshot Index Range Descriptors

## Problem

**Scenario**: Array with millions of elements, most tiny, scattered huge entries.

**Current Design Issue**:
- If we index every element: index node would be huge (millions of entries)
- Index would need to store all array elements, defeating the purpose
- Index size could exceed 1M limit

**Example**:
```
array: [
  0: "tiny",
  1: "tiny",
  ...
  999999: "tiny",
  1000000: <huge 10MB object>,
  1000001: "tiny",
  ...
]
```

If we index every element:
- Index node: `{[0]: !snap-offset 0, [1]: !snap-offset 10, ..., [1000000]: !snap-offset 10000000, ...}`
- Index size: ~8MB+ just for offsets, plus structure overhead
- **Problem**: Index is huge, and we're storing offsets for tiny elements we don't need

## Solution: Range Descriptors

**Key Idea**: Only index paths above a **size threshold**. For unindexed ranges, store a **range descriptor** indicating "scan from here".

### Range Descriptor Structure

```go
// Range descriptor in index node
{
  array: {
    !snap-range: {
      start: !snap-offset 0,        // Start offset of range
      end: !snap-offset 10000000,   // End offset of range (or size)
      threshold: 1024,              // Size threshold (bytes)
      indexed: [                    // Only paths above threshold
        1000000: !snap-offset 5000000  // Huge entry at index 1000000
      ]
    }
  }
}
```

**Or simpler**:
```go
{
  array: !snap-range 0              // Range starts at offset 0, scan from here
  "array[1000000]": !snap-offset 5000000  // Only index huge entry
}
```

### Index Building Logic

**Algorithm**:
1. Traverse snapshot in pre-order DFS
2. For each path, calculate encoded size
3. **If size >= threshold**: Index it individually (`!snap-offset`)
4. **If size < threshold**: Don't index individually, inherit parent range
5. **For containers** (arrays/objects): Store range descriptor if children are mostly unindexed

**Size Threshold**: Configurable (e.g., 1KB, 4KB, 16KB)
- **Smaller threshold**: More granular indexing, larger index
- **Larger threshold**: Fewer indexed paths, smaller index, more scanning

### Range Descriptor Format Options

#### Option 1: Tagged Range Value
```go
{
  array: !snap-range {
    start: 0,
    end: 10000000,
    threshold: 1024
  }
}
```

**Pros**: Self-contained, includes metadata
**Cons**: More complex structure

#### Option 2: Simple Range Tag
```go
{
  array: !snap-range 0              // Range starts at offset 0
  "array[1000000]": !snap-offset 5000000  // Indexed entry
}
```

**Pros**: Simple, minimal overhead
**Cons**: Need to track range end separately (or scan until next indexed path)

#### Option 3: Range with Indexed Subset
```go
{
  array: {
    _range: !snap-range 0           // Range start
    _threshold: !snap-threshold 1024
    1000000: !snap-offset 5000000   // Only indexed entries
  }
}
```

**Pros**: Clear separation, can list indexed entries
**Cons**: Special field names (`_range`, `_threshold`)

#### Option 4: Range Boundary Tags (Recommended)
```go
{
  array: {
    !snap-range-start: 0            // Range starts here
    !snap-range-threshold: 1024     // Size threshold
    1000000: !snap-offset 5000000    // Indexed entry
    !snap-range-end: 10000000       // Range ends here (optional)
  }
}
```

**Pros**: Uses tag system, clear semantics
**Cons**: Multiple tags per range

### Reading Logic

**When reading a path**:

1. **Check if path is directly indexed**: `GetKPath("array[1000000]")` → returns `!snap-offset`
   - ✅ Use offset directly

2. **If not indexed, find parent range**: `GetKPath("array[500]")` → not found
   - Navigate to parent: `GetKPath("array")` → returns `!snap-range-start: 0`
   - **Scan from range start**: Seek to offset 0, parse tokens until we find `[500]`

3. **Range boundaries**: 
   - **Start**: `!snap-range-start` offset
   - **End**: Next indexed path's offset, or `!snap-range-end`, or end of container

**Scanning Algorithm**:
```go
func (r *SnapshotReader) ReadPathFromRange(kindedPath string) (*ir.Node, error) {
    // 1. Check if directly indexed
    offset, err := r.index.GetPathOffset(kindedPath)
    if err == nil {
        return r.readAtOffset(offset)
    }
    
    // 2. Find parent range
    parentPath := getParentPath(kindedPath)  // "array[500]" → "array"
    rangeStart, rangeEnd, err := r.index.GetRange(parentPath)
    if err != nil {
        return nil, err  // No range, path doesn't exist
    }
    
    // 3. Scan from range start
    return r.scanToPath(rangeStart, rangeEnd, kindedPath)
}
```

### Index Building Implementation

**During snapshot encoding**:

```go
type IndexBuilder struct {
    threshold int64  // Size threshold (bytes)
    offsets   map[string]int64
    ranges    map[string]RangeInfo
}

type RangeInfo struct {
    Start     int64
    End       int64  // Or -1 if unknown
    Threshold int64
}

func (b *IndexBuilder) AddPath(path string, encodedSize int64, offset int64) {
    if encodedSize >= b.threshold {
        // Index individually
        b.offsets[path] = offset
    } else {
        // Don't index, will be in parent range
        // Track for range calculation
        parentPath := getParentPath(path)
        b.updateRange(parentPath, offset, encodedSize)
    }
}

func (b *IndexBuilder) BuildIndexNode(dataNode *ir.Node) (*ir.Node, error) {
    indexNode := &ir.Node{Type: ObjectType}
    
    // Add indexed paths
    for path, offset := range b.offsets {
        setPathOffset(indexNode, path, offset)
    }
    
    // Add range descriptors for containers with unindexed children
    for path, rangeInfo := range b.ranges {
        setRangeDescriptor(indexNode, path, rangeInfo)
    }
    
    return indexNode, nil
}
```

### Size Threshold Selection

**Factors**:
- **Index size limit**: Keep index < 1M
- **Scan cost**: Larger threshold = more scanning
- **Access patterns**: If most accesses are to large entries, higher threshold is fine

**Recommendation**: **4KB threshold**
- Small enough to index moderately-sized entries
- Large enough to avoid indexing millions of tiny strings
- Reasonable scan cost for unindexed paths

### Example: Large Array

**Snapshot Data**:
```
array: [
  0: "tiny",
  1: "tiny",
  ...
  999999: "tiny",           // Each ~10 bytes
  1000000: {huge: "..."},   // 10MB
  1000001: "tiny",
  ...
]
```

**Index Node** (with 4KB threshold):
```
{
  array: {
    !snap-range-start: 0
    !snap-range-threshold: 4096
    1000000: !snap-offset 5000000    // Only huge entry indexed
  }
}
```

**Index Size**: ~100 bytes (just one indexed entry + range descriptor)
**vs. Full Index**: ~8MB (millions of offsets)

### Trade-offs

**Pros**:
- ✅ **Small index**: Only indexes "interesting" paths
- ✅ **Scalable**: Works with millions of elements
- ✅ **Flexible**: Threshold configurable
- ✅ **Backward compatible**: Can still index everything if threshold = 0

**Cons**:
- ⚠️ **Scan cost**: Reading unindexed paths requires scanning
- ⚠️ **Complexity**: More complex index building and reading logic
- ⚠️ **Range boundaries**: Need to track where ranges end

### Implementation Considerations

1. **Range End Detection**:
   - **Option A**: Store `!snap-range-end` explicitly
   - **Option B**: Use next indexed path's offset
   - **Option C**: Use container size from parent

2. **Nested Ranges**:
   - What if `array[1000000]` itself contains a large array?
   - **Solution**: Range descriptors can nest, scan within range

3. **Threshold Configuration**:
   - **Per-snapshot**: Store threshold in index metadata
   - **Global**: Use same threshold for all snapshots
   - **Per-container**: Different thresholds for different containers

4. **Index Size Monitoring**:
   - Track index size during building
   - If index approaches 1M limit, increase threshold dynamically

### Recommended Format

**Simple Range Tag** (Option 2, extended):
```go
{
  array: {
    !snap-range: 0              // Range starts at offset 0
    !snap-threshold: 4096        // Size threshold
    1000000: !snap-offset 5000000  // Indexed entry
  }
}
```

**Reading**:
- If path directly indexed → use offset
- If not → find parent with `!snap-range`, scan from that offset until path found or range end

**Benefits**:
- Simple tag system
- Minimal overhead
- Clear semantics
- Easy to implement
