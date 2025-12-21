# Analysis: Index Entries vs ir.Node for Snapshot Indexing

## Critical Question

**Do we need full `ir.Node` structures for the index, or can we build it with just `Index` entries?**

**User's Insight**: For large containers (millions of elements), we can't index every element. We need **range descriptors** that handle breadth (sibling elements) as well as depth (nested structures).

## Current Index Structure

From `internal/snap/index.go`:
```go
type Index struct {
    StartField *string
    Start      int
    End        int
    Offset     int64
    Size       int64
    
    ParentField       string
    ParentSparseIndex int
    ParentIndex       int
    Parent            *Index
    
    Children []Index
}
```

**This is a tree of Index entries** - but is this sufficient?

## Range Descriptor Requirements

From `snapshot_index_range_descriptors.md`:

**Problem**: Array with millions of elements
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

**Solution**: Range descriptors
```
{
  array: {
    !snap-range: 0              // Range starts at offset 0
    !snap-threshold: 4096       // Size threshold
    1000000: !snap-offset 5000000  // Only huge entry indexed
  }
}
```

**Key Requirements**:
1. ✅ **Range boundaries**: Track `[from, to)` for unindexed ranges
2. ✅ **Size calculation**: Calculate size of ranges during encoding
3. ✅ **Threshold application**: Only index entries above threshold
4. ✅ **State across breadth**: Track which elements are in current range
5. ✅ **Range finalization**: When range exceeds threshold, finalize it

## What's Needed for Range Descriptors

### During Encoding

**State needed**:
1. **Current range state**: 
   - Range start offset
   - Range start index (which element started the range)
   - Accumulated size of current range
   - List of elements in current range

2. **Size calculation**:
   - Need to calculate encoded size of each element
   - Accumulate sizes to know when to finalize range

3. **Range finalization**:
   - When range size >= threshold, write chunk
   - Create `!snap-range` node with descriptor `[from, to, offset, size]`
   - Reset range state

**Question**: Can `StreamEncoder` provide this?

## StreamEncoder Capabilities

### What StreamEncoder Provides

```go
enc := parse.NewStreamEncoder(writer)

enc.BeginArray()
offset := enc.Offset()  // ✅ Can track offset

for i := 0; i < 1000000; i++ {
    enc.WriteInt(i)
    // ❌ Can't calculate encoded size of what we just wrote
    // ❌ Can't know when to finalize range
    // ❌ No way to track "current range state"
}

enc.EndArray()
```

**Missing**:
- ❌ **Size calculation**: Can't calculate encoded size of values as we write
- ❌ **Range state**: No way to track "current range" (which elements, accumulated size)
- ❌ **Range finalization**: No way to know when to finalize a range

### What We Actually Need

**For range descriptors, we need**:

1. **Size calculation during encoding**:
   ```go
   // Need to know: how many bytes did WriteInt() write?
   size := enc.SizeOfLastWrite()  // Doesn't exist!
   ```

2. **Range state tracking**:
   ```go
   // Need to track: current range start, accumulated size, elements
   rangeState := &RangeState{
       StartOffset: enc.Offset(),
       StartIndex: 0,
       AccumulatedSize: 0,
       Elements: []ElementInfo{},
   }
   ```

3. **Range finalization**:
   ```go
   // Need to: write chunk, create !snap-range node, reset state
   if rangeState.AccumulatedSize >= threshold {
       chunkOffset := writeChunk(...)
       createRangeDescriptor(rangeState, chunkOffset)
       resetRangeState()
   }
   ```

## The Real Question

**Does StreamEncoder need to support range descriptors, or is that a higher-level concern?**

### Option 1: StreamEncoder Provides Size Calculation

```go
type StreamEncoder struct {
    // ...
}

func (e *StreamEncoder) WriteInt(value int64) error {
    // Write value
    bytesWritten := e.writeInt(value)
    
    // Track size
    e.lastWriteSize = bytesWritten
    return nil
}

func (e *StreamEncoder) LastWriteSize() int64 {
    return e.lastWriteSize
}
```

**Then higher-level code can**:
```go
enc := parse.NewStreamEncoder(writer)
rangeState := &RangeState{...}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    startOffset := enc.Offset()
    enc.WriteInt(i)
    size := enc.LastWriteSize()  // ✅ Can calculate size
    
    rangeState.AccumulatedSize += size
    if rangeState.AccumulatedSize >= threshold {
        // Finalize range
        finalizeRange(rangeState, startOffset)
    }
}
```

**Pros**:
- ✅ StreamEncoder provides size calculation
- ✅ Higher-level code handles range logic

**Cons**:
- ⚠️ Still need to track range state externally
- ⚠️ Still need to handle range finalization externally

### Option 2: Index Building is Higher-Level (Current Plan)

**StreamEncoder provides**:
- ✅ Explicit boundaries (`Begin/End`)
- ✅ Queryable offsets
- ✅ Queryable state (depth, path, etc.)

**Index building code handles**:
- ✅ Size calculation (can measure before/after offsets)
- ✅ Range state tracking
- ✅ Range finalization
- ✅ Building `ir.Node` index structure

**But**: How do we calculate size if we can't measure what was written?

## Size Calculation Problem

### The Challenge

**During encoding**:
```go
enc.WriteInt(42)
// How many bytes did that write?
// We can't know without:
// 1. Measuring before/after offset (if encoder tracks it)
// 2. Calculating size ourselves (duplicate encoder logic)
// 3. Encoder providing size (new API)
```

### Current TokenSink Approach

Looking at `token/sink.go`:
- `TokenSink.Offset()` tracks bytes written
- Can calculate size by: `size = offsetAfter - offsetBefore`

**But**: This only works if we track offsets before/after each write.

### StreamEncoder Solution

```go
enc := parse.NewStreamEncoder(writer)

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    enc.WriteInt(i)
    offsetAfter := enc.Offset()
    size := offsetAfter - offsetBefore  // ✅ Can calculate size!
    
    // Track range state
    rangeState.AccumulatedSize += size
    if rangeState.AccumulatedSize >= threshold {
        finalizeRange(rangeState)
    }
}
```

**This works!** ✅

## Range State Tracking

### What State Do We Need?

**For building range descriptors**:

```go
type RangeState struct {
    StartOffset int64      // Where range starts in snapshot
    StartIndex  int        // Which element index started range
    AccumulatedSize int64  // Total size of elements in range
    ElementCount int       // How many elements in range
    // For objects: also track fields
    Fields []string
    Values []*ir.Node  // Or just track that we need to write them
}
```

**Question**: Can we build this with StreamEncoder?

**Answer**: ✅ **YES**, but we need to track it externally:

```go
enc := parse.NewStreamEncoder(writer)
rangeState := &RangeState{...}

enc.BeginArray()
rangeState.StartOffset = enc.Offset()
rangeState.StartIndex = 0

for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    enc.WriteInt(i)
    size := enc.Offset() - offsetBefore
    
    rangeState.AccumulatedSize += size
    rangeState.ElementCount++
    
    if rangeState.AccumulatedSize >= threshold {
        // Finalize range: write chunk, create !snap-range node
        chunkOffset := writeChunk(...)
        rangeNode := createRangeNode(rangeState, chunkOffset)
        indexNode.Values = append(indexNode.Values, rangeNode)
        
        // Reset range state
        rangeState = &RangeState{
            StartOffset: enc.Offset(),
            StartIndex: i + 1,
        }
    }
}

// Finalize any remaining range
if rangeState.ElementCount > 0 {
    finalizeRange(rangeState)
}
```

## Index Structure: Index Entries vs ir.Node

### Current Design: Index as ir.Node

From `snapshot_ir_node_index.md`:
- Index is an `ir.Node` (mirrors snapshot structure)
- Contains `!snap-offset` tags for indexed paths
- Contains `!snap-range` nodes for ranges

**Example**:
```go
// Index node structure
indexNode := &ir.Node{
    Type: ir.ObjectType,
    Values: []*ir.Node{
        {
            Key: ir.FromString("array"),
            Val: &ir.Node{
                Type: ir.ObjectType,
                Values: []*ir.Node{
                    {
                        Key: ir.FromString("!snap-range"),
                        Val: rangeNode,  // Contains [from, to, offset, size]
                    },
                    {
                        Key: ir.FromInt(1000000),
                        Val: &ir.Node{
                            Tag: "!snap-offset",
                            Int64: &offset,
                        },
                    },
                },
            },
        },
    },
}
```

### Can We Build This with StreamEncoder?

**Building index node structure**:

```go
// We're encoding the SNAPSHOT, not the index
enc := parse.NewStreamEncoder(snapshotWriter)

// But we need to BUILD the index node
indexNode := &ir.Node{Type: ir.ObjectType}

enc.BeginArray()
// Track range state
rangeState := &RangeState{...}

for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    enc.WriteInt(i)
    size := enc.Offset() - offsetBefore
    
    if size >= threshold {
        // Index individually
        addIndexEntry(indexNode, i, offsetBefore, size)
    } else {
        // Add to range
        rangeState.AccumulatedSize += size
        if rangeState.AccumulatedSize >= threshold {
            // Finalize range, add to index node
            rangeNode := finalizeRange(rangeState)
            addRangeToIndex(indexNode, rangeNode)
        }
    }
}
```

**Key Point**: We're building an `ir.Node` index structure **in parallel** with encoding the snapshot.

## The Answer

### Do We Need ir.Node for Index?

**YES** - The index structure IS an `ir.Node`:
- Contains `!snap-offset` tagged values
- Contains `!snap-range` nodes with descriptors
- Mirrors snapshot structure

### Can StreamEncoder Help Build It?

**YES** - StreamEncoder provides:
- ✅ Explicit boundaries (know when structures start/end)
- ✅ Queryable offsets (can calculate sizes)
- ✅ Queryable state (can track path, depth, etc.)

**But we still need**:
- ⚠️ **External range state tracking** (track current range)
- ⚠️ **Size calculation** (offset difference, or encoder provides it)
- ⚠️ **Index node building** (build `ir.Node` structure in parallel)

## Updated Understanding

**Index building requires**:
1. ✅ **StreamEncoder** - for encoding snapshot with explicit boundaries
2. ✅ **Range state tracking** - external state (which elements, accumulated size)
3. ✅ **Size calculation** - from offset differences
4. ✅ **Index node building** - build `ir.Node` structure in parallel

**StreamEncoder solves the impedance mismatch for**:
- ✅ Explicit boundaries
- ✅ Queryable offsets
- ✅ Queryable state

**But range descriptors require additional logic**:
- ⚠️ Range state tracking (external)
- ⚠️ Size calculation (offset differences)
- ⚠️ Range finalization (external)

**Conclusion**: StreamEncoder provides the foundation, but range descriptors need additional state management on top.
