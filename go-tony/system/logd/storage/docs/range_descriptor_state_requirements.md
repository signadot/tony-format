# Range Descriptor State Requirements

## Critical Insight

**Index building requires tracking state across BOTH dimensions**:
1. **Depth**: Nested structures (parent-child relationships)
2. **Breadth**: Sibling elements within containers (range descriptors)

## Current Implementation Analysis

From `internal/snap/from_ir.go`:

### objectRangeState Structure

```go
type objectRangeState struct {
    rangeStart      int64        // Offset where current range starts
    rangeStartIndex int          // Which element index started range
    rangeFields     []*ir.Node   // Fields in current range
    rangeValues     []*ir.Node   // Values in current range
}
```

**Key State**:
- ✅ **Breadth tracking**: Which elements are in current range (`rangeFields`, `rangeValues`)
- ✅ **Size accumulation**: Track accumulated size (calculated from `rangeValues`)
- ✅ **Range boundaries**: Track start offset and start index

### Range Finalization Logic

```go
func finalizeObjectRange(
    state *objectRangeState,
    indexNode *ir.Node,      // Building ir.Node index structure
    isSparseArray bool,
    chunks *[]chunkInfo,
    offset *int64,
) error {
    // 1. Build range data from accumulated elements
    rangeData := buildRangeData(state.rangeValues, state.rangeFields, isSparseArray)
    
    // 2. Create chunk
    chunk := chunkInfo{
        data:   rangeData,
        offset: state.rangeStart,
        size:   int64(len(rangeData)),
    }
    
    // 3. Create !snap-range node (ir.Node structure)
    rangeNode := ir.FromSlice([]*ir.Node{
        &ir.Node{Type: ir.NumberType, Int64: &state.rangeStart},
        &ir.Node{Type: ir.NumberType, Int64: &chunk.size},
    })
    rangeNode.Tag = fmt.Sprintf("!snap-range(%d,%d)", rangeStartIdx, rangeEndIdx)
    
    // 4. Add to index node (ir.Node structure)
    indexNode.Values = append(indexNode.Values, rangeNode)
    
    // 5. Reset range state
    state.rangeStart = *offset
    state.rangeStartIndex = len(indexNode.Values)
    state.rangeFields = nil
    state.rangeValues = nil
}
```

**Key Requirements**:
1. ✅ **Accumulate elements**: Need to collect `rangeFields` and `rangeValues` (full `ir.Node` structures!)
2. ✅ **Calculate size**: Need to encode range data to calculate size
3. ✅ **Build ir.Node**: Index structure IS an `ir.Node`, not just Index entries
4. ✅ **Track breadth**: Need to know which elements are in current range

## What StreamEncoder Provides

### For Depth (Nested Structures)

```go
enc := parse.NewStreamEncoder(writer)

enc.BeginObject()  // ✅ Explicit boundary
path := enc.CurrentPath()  // ✅ Queryable
parentPath := enc.ParentPath()  // ✅ Queryable
offset := enc.Offset()  // ✅ Queryable

enc.WriteKey("key")
enc.BeginObject()  // Nested structure
// Can track depth, parent relationships
```

**✅ SOLVED**: Explicit boundaries + queryable state enable depth tracking

### For Breadth (Range Descriptors)

```go
enc := parse.NewStreamEncoder(writer)
rangeState := &objectRangeState{...}

enc.BeginArray()
rangeState.rangeStart = enc.Offset()
rangeState.rangeStartIndex = 0

for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    // ❌ PROBLEM: We're writing to snapshot, not building ir.Node
    enc.WriteInt(i)
    
    offsetAfter := enc.Offset()
    size := offsetAfter - offsetBefore  // ✅ Can calculate size
    
    // ❌ PROBLEM: We don't have the ir.Node for this element!
    // We need rangeValues to contain ir.Node structures
    // But we're writing directly to snapshot, not building nodes
    
    rangeState.AccumulatedSize += size
    // ❌ Can't add to rangeValues - we don't have the ir.Node!
}
```

**❌ PROBLEM**: StreamEncoder writes to snapshot, but range descriptors need `ir.Node` structures!

## The Real Issue

### Two Parallel Processes

**Process 1: Encoding Snapshot**
```go
enc := parse.NewStreamEncoder(snapshotWriter)
enc.WriteInt(42)  // Writes to snapshot
```

**Process 2: Building Index**
```go
indexNode := &ir.Node{...}
// Need to build ir.Node structure for index
// But we're writing to snapshot, not building nodes!
```

**The Problem**: 
- StreamEncoder writes directly to snapshot (bytes)
- Range descriptors need `ir.Node` structures (for `rangeValues`)
- We can't get `ir.Node` from bytes we've written

### Current Implementation Approach

Looking at `from_ir.go`, the current implementation:
1. **Has the `ir.Node` already** (from `buildIndexRecursive(node *ir.Node, ...)`)
2. **Encodes it** to calculate size
3. **Builds index** in parallel

**Key**: The current implementation works on `ir.Node` structures, not streaming!

## What This Means for StreamEncoder

### Option 1: StreamEncoder Can't Build Range Descriptors

**If we're streaming** (don't have `ir.Node` structures):
- ❌ Can't accumulate `rangeValues` (need `ir.Node` structures)
- ❌ Can't build `ir.Node` index structure
- ❌ Can't create `!snap-range` nodes

**Conclusion**: Range descriptors require `ir.Node` structures, not just streaming.

### Option 2: Hybrid Approach

**For small containers** (fit in memory):
- ✅ Use StreamEncoder to encode snapshot
- ✅ Build Index entries (not full `ir.Node` structures)
- ✅ Simple, works fine

**For large containers** (don't fit in memory):
- ⚠️ Need `ir.Node` structures for range descriptors
- ⚠️ Can't use pure streaming (need to build nodes)
- ⚠️ Need different approach

### Option 3: StreamEncoder Provides Size Calculation

**If StreamEncoder can provide size**:
```go
enc := parse.NewStreamEncoder(writer)

enc.BeginArray()
rangeState := &RangeState{
    StartOffset: enc.Offset(),
    StartIndex: 0,
    AccumulatedSize: 0,
    ElementOffsets: []int64{},  // Track offsets instead of ir.Nodes
}

for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    enc.WriteInt(i)
    size := enc.Offset() - offsetBefore  // ✅ Can calculate size
    
    rangeState.AccumulatedSize += size
    rangeState.ElementOffsets = append(rangeState.ElementOffsets, offsetBefore)
    
    if rangeState.AccumulatedSize >= threshold {
        // Finalize range using offsets
        finalizeRangeFromOffsets(rangeState)
    }
}
```

**But**: Range descriptors need `[from, to, offset, size]` - we can build this from offsets!

**However**: The index structure IS an `ir.Node` - we still need to build that.

## Updated Understanding

### Index Structure IS ir.Node

From `internal/snap/doc.go`:
- Index is a tree of `ir.Node` structures
- Contains `!snap-range` nodes (which are `ir.Node` structures)
- Contains `!snap-offset` tagged values

**Conclusion**: The index structure MUST be `ir.Node`, not just Index entries.

### What StreamEncoder Can Provide

**For simple indexing** (no range descriptors):
- ✅ Explicit boundaries
- ✅ Queryable offsets
- ✅ Can build Index entries

**For range descriptors**:
- ✅ Can calculate sizes (offset differences)
- ✅ Can track range state (external)
- ⚠️ **But**: Still need to build `ir.Node` index structure
- ⚠️ **But**: Range descriptors reference `ir.Node` structures in `rangeValues`

### The Real Question

**Can we build range descriptors without having full `ir.Node` structures?**

**Answer**: **YES**, but we need to track different information:

**Instead of**:
```go
rangeState.rangeValues = []*ir.Node{...}  // Full nodes
```

**We can track**:
```go
rangeState.elementOffsets = []int64{...}  // Just offsets
rangeState.elementSizes = []int64{...}    // Just sizes
rangeState.elementIndices = []int{...}    // Just indices
```

**Then build range descriptor**:
```go
rangeNode := &ir.Node{
    Tag: "!snap-range(0,100)",
    Values: []*ir.Node{
        {Type: ir.NumberType, Int64: &startOffset},
        {Type: ir.NumberType, Int64: &rangeSize},
    },
}
```

**This works!** ✅

## Conclusion

### StreamEncoder CAN Support Range Descriptors

**What we need**:
1. ✅ **Size calculation**: From offset differences (StreamEncoder provides)
2. ✅ **Range state tracking**: External state (we provide)
3. ✅ **Range boundaries**: From offsets (StreamEncoder provides)
4. ✅ **Index node building**: Build `ir.Node` structure (we provide)

**What we DON'T need**:
- ❌ Full `ir.Node` structures for each element (can use offsets instead)
- ❌ Pre-built nodes (can build index structure from offsets)

**Key Insight**: 
- Range descriptors need `[from, to, offset, size]` tuples
- We can build these from offsets tracked during encoding
- Index structure IS `ir.Node`, but we can build it from offset information

**StreamEncoder provides the foundation** - explicit boundaries and queryable offsets enable building range descriptors!
