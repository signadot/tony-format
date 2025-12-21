# Snapshot Index Design Summary: Range Descriptors

## Problem Statement

**Challenge**: Arrays with millions of elements, most tiny, scattered huge entries.

**Example**:
```
array: [
  0: "tiny",        // ~10 bytes each
  1: "tiny",
  ...
  999999: "tiny",
  1000000: {huge},  // 10MB object
  1000001: "tiny",
  ...
]
```

**Issue**: If we index every element:
- Index node would be huge (millions of entries)
- Index size could exceed 1M limit
- We're storing offsets for tiny elements we rarely need

## Solution: Range Descriptors with Size Threshold

**Key Idea**: Only index paths above a **size threshold** (e.g., 4KB). For unindexed ranges, store a **range descriptor** indicating "scan from here".

### Index Structure

**Index Node** (with 4KB threshold):
```
{
  array: {
    !snap-range: 0              // Range starts at offset 0
    !snap-threshold: 4096       // Size threshold
    1000000: !snap-offset 5000000  // Only huge entry indexed
  }
}
```

**Index Size**: ~100 bytes (one indexed entry + range descriptor)
**vs. Full Index**: ~8MB (millions of offsets)

### Reading Logic

**When reading a path**:

1. **If directly indexed**: `ReadPath("array[1000000]")`
   - ✅ Use `!snap-offset` directly
   - Fast O(depth) lookup

2. **If not indexed**: `ReadPath("array[500]")`
   - Find parent range: `GetRange("array")` → `startOffset: 0`
   - **Scan from range start**: Parse tokens from offset 0, navigate to `[500]`
   - Slower but acceptable for small entries

### Size Threshold

**Recommendation**: **4KB threshold**
- Small enough to index moderately-sized entries
- Large enough to avoid indexing millions of tiny strings
- Reasonable scan cost for unindexed paths

**Configurable**: Can be adjusted per-snapshot or globally

## Implementation Impact

### Index Building
- **Calculate encoded size** for each path during encoding
- **Apply threshold**: Only index paths >= threshold
- **Create range descriptors** for containers with unindexed children

### Index Navigation
- **Direct lookup**: For indexed paths (fast)
- **Range scanning**: For unindexed paths (slower but necessary)

### Work Estimate
- **Increased complexity**: ~4-5 days additional work
- **Total**: ~18-26 days (vs. ~14-21 days without ranges)
- **Essential**: Required for handling large arrays

## Benefits

✅ **Small index**: Only indexes "interesting" paths  
✅ **Scalable**: Works with millions of elements  
✅ **Flexible**: Threshold configurable  
✅ **Backward compatible**: Can still index everything if threshold = 0  

## Trade-offs

⚠️ **Scan cost**: Reading unindexed paths requires scanning  
⚠️ **Complexity**: More complex index building and reading logic  
⚠️ **Range boundaries**: Need to track where ranges end  

## Next Steps

1. **Design range descriptor format** (see `snapshot_index_range_descriptors.md`)
2. **Implement size calculation** during encoding
3. **Implement range scanning** for unindexed paths
4. **Test with large arrays** (millions of elements)
