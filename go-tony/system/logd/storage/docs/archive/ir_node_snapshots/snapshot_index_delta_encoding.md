# Snapshot Index with Delta-Encoded Offsets

## Encoding Scheme

**Pre-order DFS traversal** + **Varuint delta encoding** of offsets.

## Key Insight

If we traverse the tree in pre-order DFS order and store offsets with delta encoding:
- **Offset[i]** = where path[i] starts (delta from previous)
- **Offset[i+1]** = where path[i+1] starts
- **Size[i]** = Offset[i+1] - Offset[i] (for all but last path)

## Example

Suppose we have a snapshot with paths in pre-order DFS order:
```
"" (root)
"a"
"a.b"
"a.c"
"x"
"x.y"
```

And their byte offsets:
```
""  → offset 0
"a" → offset 100
"a.b" → offset 150
"a.c" → offset 200
"x" → offset 300
"x.y" → offset 350
```

**Delta-encoded offsets**:
```
""  → 0 (absolute)
"a" → 100 (delta from 0)
"a.b" → 50 (delta from 100)
"a.c" → 50 (delta from 150)
"x" → 100 (delta from 200)
"x.y" → 50 (delta from 300)
```

**Sizes** (computed from offsets):
```
""  → size 100 (offset["a"] - offset[""])
"a" → size 50 (offset["a.b"] - offset["a"])
"a.b" → size 50 (offset["a.c"] - offset["a.b"])
"a.c" → size 100 (offset["x"] - offset["a.c"])
"x" → size 50 (offset["x.y"] - offset["x"])
"x.y" → size ? (need snapshot end)
```

## Problem: Last Path Size

**Issue**: For the last path, we don't have Offset[i+1], so we can't compute Size[i].

**Solutions**:

### Option 1: Store Snapshot Size
Store total snapshot size separately:
```
Size[last] = snapshotSize - Offset[last]
```

### Option 2: Store Last Path Size
Store size of last path explicitly:
```
Size[last] = stored explicitly
```

### Option 3: Store End Offset
Store offset where snapshot ends:
```
Size[last] = snapshotEnd - Offset[last]
```

### Option 4: Implicit from Snapshot End
When reading snapshot, we know where it ends (from Entry or next entry):
```
Size[last] = nextEntryPos - Offset[last]
```

## Path Information

**Question**: How do we know which path each offset corresponds to?

### Option A: Store Path Names
Store path names alongside offsets:
```
Index: [
  {path: "", offsetDelta: 0},
  {path: "a", offsetDelta: 100},
  {path: "a.b", offsetDelta: 50},
  ...
]
```

**Pros**: Explicit, easy to lookup
**Cons**: Path names take space (especially for deep paths)

### Option B: Implicit from Tree Structure
Store tree structure (which keys/indices exist), paths are implicit:
```
Tree structure: {
  "" → ["a", "x"],
  "a" → ["b", "c"],
  "x" → ["y"]
}
Offsets: [0, 100, 50, 50, 100, 50]  // delta-encoded
```

**Pros**: More compact (no path strings)
**Cons**: Need to reconstruct paths from tree structure

### Option C: Store Path Segments
Store path segments (keys/indices) at each level:
```
Level 0: ["a", "x"] → offsets [0, 100]
Level 1 "a": ["b", "c"] → offsets [50, 50]
Level 1 "x": ["y"] → offsets [50]
```

**Pros**: Compact, paths reconstructable
**Cons**: More complex encoding/decoding

## Complete Encoding Format

### Format 1: Path Names + Delta Offsets + Snapshot Size

```
[Snapshot Size: varuint]
[Path Count: varuint]
For each path (pre-order DFS):
  [Path Length: varuint]
  [Path Bytes: bytes]
  [Offset Delta: varuint]  // delta from previous offset
```

**Lookup**: Binary search by path name
**Size**: Size[i] = Offset[i+1] - Offset[i], Size[last] = snapshotSize - Offset[last]

### Format 2: Tree Structure + Delta Offsets + Snapshot Size

```
[Snapshot Size: varuint]
[Tree Structure: encoded tree]
[Offset Count: varuint]
For each offset (pre-order DFS):
  [Offset Delta: varuint]
```

**Lookup**: Traverse tree to find path, use index in traversal order
**Size**: Same as Format 1

### Format 3: Path Segments + Delta Offsets + Snapshot Size

```
[Snapshot Size: varuint]
[Root Children Count: varuint]
For each root child:
  [Key/Index: encoded]
  [Offset Delta: varuint]
  [Has Children: bool]
  If has children:
    [Children Count: varuint]
    For each child:
      [Key/Index: encoded]
      [Offset Delta: varuint]
      ...
```

**Lookup**: Traverse tree structure
**Size**: Same as Format 1

## Size Calculation

**Yes, delta-encoded offsets give us enough info to find sizes**, with one caveat:

✅ **For all paths except the last**: Size[i] = Offset[i+1] - Offset[i]
⚠️ **For the last path**: Need snapshot size or end offset

**Reconstruction**:
```go
func getSizes(deltaOffsets []uint64, snapshotSize uint64) []uint64 {
    offsets := make([]uint64, len(deltaOffsets))
    sizes := make([]uint64, len(deltaOffsets))
    
    // Reconstruct absolute offsets
    offsets[0] = deltaOffsets[0]
    for i := 1; i < len(deltaOffsets); i++ {
        offsets[i] = offsets[i-1] + deltaOffsets[i]
    }
    
    // Compute sizes
    for i := 0; i < len(deltaOffsets)-1; i++ {
        sizes[i] = offsets[i+1] - offsets[i]
    }
    sizes[len(deltaOffsets)-1] = snapshotSize - offsets[len(deltaOffsets)-1]
    
    return sizes
}
```

## Benefits of Delta Encoding

1. **Compact**: Deltas are typically small (paths are close together)
2. **Efficient**: Varuint encoding makes small deltas very compact
3. **Size derivation**: Can compute sizes from offsets (except last)
4. **Pre-order DFS**: Natural traversal order matches Tony format structure

## Example Encoding Size

**Without delta encoding** (absolute offsets):
```
Offset 0: 0 bytes (varuint)
Offset 100: ~1 byte (varuint)
Offset 150: ~1 byte (varuint)
Offset 200: ~1 byte (varuint)
Offset 300: ~2 bytes (varuint)
Offset 350: ~2 bytes (varuint)
Total: ~8 bytes
```

**With delta encoding**:
```
Delta 0: 0 bytes (varuint)
Delta 100: ~1 byte (varuint)
Delta 50: ~1 byte (varuint)
Delta 50: ~1 byte (varuint)
Delta 100: ~1 byte (varuint)
Delta 50: ~1 byte (varuint)
Total: ~5 bytes
```

**Savings**: ~37% in this example (more for larger offsets)

## Recommendation

**Yes, delta-encoded offsets give us enough info**, but we need:
1. **Snapshot size** (or end offset) for the last path
2. **Path information** (names, tree structure, or segments) to map offsets to paths

**Suggested Format**:
- Store snapshot size at start
- Store path names (or tree structure) + delta-encoded offsets
- Compute sizes on-the-fly when reading index
