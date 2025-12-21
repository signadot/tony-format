# Streaming Indexing: Correct Understanding

## The Actual Process

### Current Implementation (from_ir.go, from_snap.go)

**WriteFromIR** (from `ir.Node` in memory):
1. Build index from `ir.Node` (has full structure)
2. Collect chunks
3. Write index, then chunks sequentially

**FromSnap** (transform existing snapshot):
1. Load index (in-memory)
2. Reconstruct full `ir.Node` from chunks (loads everything into memory)
3. Apply patches
4. Call `WriteFromIR` to write new snapshot

### Future Implementation (Streaming)

**Input**:
- Existing index (in-memory `ir.Node`)
- Sequence of chunks (from existing snapshot file)

**Process**:
1. Read chunks incrementally with `StreamDecoder`
2. Process incrementally (apply patches, transform)
3. Write new chunks incrementally with `StreamEncoder` (write once, sequentially)
4. Build new index incrementally as we write

**Output**:
- New sequence of chunks (written once, sequentially)
- New index (written after chunks)

## The Flow

```
Existing Snapshot:
  [4 bytes: index length][index node][chunk1][chunk2][chunk3]...

Streaming Indexing Process:
  1. Read index (in-memory)
  2. For each chunk in existing snapshot:
     a. Read chunk with StreamDecoder
     b. Process tokens (apply patches, transform)
     c. Write to new snapshot with StreamEncoder
     d. Track offsets, build index entries/range descriptors
  3. Write new chunks sequentially (already done in step 2c)
  4. Write new index

New Snapshot:
  [4 bytes: index length][new index node][new chunk1][new chunk2][new chunk3]...
```

## What StreamEncoder/StreamDecoder Provide

### StreamDecoder (Reading Existing Chunks)

```go
dec := parse.NewStreamDecoder(chunkReader)

// Read tokens from existing chunk
tok, err := dec.ReadToken()

// Query state
depth := dec.Depth()           // Nested depth
path := dec.CurrentPath()       // Current path (e.g., "array[42]")
key := dec.CurrentKey()         // Object key (if in object)
index := dec.CurrentIndex()      // Array index (if in array)
offset := dec.Offset()           // Offset within chunk being read
```

**Purpose**: Read existing chunks, provide queryable state for processing

### StreamEncoder (Writing New Chunks)

```go
enc := parse.NewStreamEncoder(chunkWriter)

// Write tokens to new chunk
enc.WriteToken(tok)

// Query state
depth := enc.Depth()            // Nested depth
path := enc.CurrentPath()       // Current path
key := enc.CurrentKey()         // Object key
index := enc.CurrentIndex()     // Array index
offset := enc.Offset()          // Offset in new snapshot (tracks position)
```

**Purpose**: Write new chunks sequentially, provide queryable state for index building

## Range Descriptors: The Real Question

**User's Question**:
> "When a container contains more elements than can be fit in memory, we cannot store an index entry for each element -- there's needs to be a way to handle state across the breadth of a container as well as the depth."

**What this means**:
- When processing a large container (millions of elements)
- We read chunks with StreamDecoder
- We write new chunks with StreamEncoder
- We need to build range descriptors (not individual index entries for each element)
- **Question**: Do StreamEncoder/StreamDecoder provide enough state to build range descriptors?

## What We Need for Range Descriptors

### Range Descriptor Structure

```go
!snap-range(from, to) [offset, size]
```

**Requirements**:
1. ✅ **Range boundaries**: `from`, `to` (element indices)
2. ✅ **Chunk offset**: Where chunk starts in new snapshot
3. ✅ **Chunk size**: Size of chunk
4. ✅ **Breadth tracking**: Which elements are siblings (same depth, sequential)
5. ✅ **Depth tracking**: Nested structure

## What StreamEncoder Provides

### For Breadth (Sibling Elements)

```go
enc.BeginArray()  // Large array container
rangeStartIndex := 0
rangeStartOffset := enc.Offset()

for i := 0; i < 1000000; i++ {
    enc.WriteInt(i)
    
    currentIndex := enc.CurrentIndex()  // ✅ Knows array index
    currentOffset := enc.Offset()       // ✅ Knows offset in new snapshot
    
    // Track breadth: currentIndex tells us which element
    // Track accumulated size: currentOffset - rangeStartOffset
    
    if shouldFinalizeRange(currentOffset - rangeStartOffset) {
        // Create range descriptor
        rangeDesc := RangeDescriptor{
            From: rangeStartIndex,
            To: currentIndex + 1,
            Offset: rangeStartOffset,
            Size: currentOffset - rangeStartOffset,
        }
        indexBuilder.AddRangeDescriptor(rangeDesc)
        
        // Start new range
        rangeStartIndex = currentIndex + 1
        rangeStartOffset = currentOffset
    }
}
```

**StreamEncoder provides**:
- ✅ `CurrentIndex()` - knows which element (breadth)
- ✅ `CurrentKey()` - knows object key (breadth)
- ✅ `Offset()` - tracks position in new snapshot
- ✅ `Depth()` - knows nested depth

### For Depth (Nested Structures)

```go
enc.BeginObject()  // Container
depth := enc.Depth()  // ✅ Knows depth

enc.WriteKey("nested")
enc.BeginArray()  // Nested container
nestedDepth := enc.Depth()  // ✅ Knows nested depth

// Process nested elements...
enc.EndArray()
enc.EndObject()
```

**StreamEncoder provides**:
- ✅ `Depth()` - knows nested depth
- ✅ Explicit boundaries - `Begin/End` pairs

## The Answer

**YES** - StreamEncoder/StreamDecoder provide all the queryable state needed:

1. ✅ **Breadth**: `CurrentIndex()`, `CurrentKey()` - know which sibling element
2. ✅ **Depth**: `Depth()`, explicit `Begin/End` boundaries - know nested structure
3. ✅ **Offsets**: `Offset()` - tracks position in new snapshot
4. ✅ **Sizes**: Can calculate from offset differences

**What we need to add**:
- ⚠️ **External range state tracking**: Track current range (start index, start offset, accumulated size)
- ⚠️ **Range finalization logic**: When to create range descriptor (threshold-based)

But the **foundation is there** - StreamEncoder/StreamDecoder provide all the queryable state needed!

## Example: Building Range Descriptors

```go
func buildIndexWithRanges(
    dec *parse.StreamDecoder,  // Read existing chunks
    enc *parse.StreamEncoder,  // Write new chunks
    indexBuilder *IndexBuilder,
) error {
    var rangeState *RangeState
    
    for {
        tok, err := dec.ReadToken()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        
        // Process token (apply patches, transform)
        processedTok := processToken(tok)
        
        // Write to new snapshot
        enc.WriteToken(processedTok)
        
        // Track for index building
        if enc.IsInArray() {
            currentIndex := enc.CurrentIndex()
            currentOffset := enc.Offset()
            
            if rangeState == nil {
                // Start new range
                rangeState = &RangeState{
                    StartIndex: currentIndex,
                    StartOffset: currentOffset,
                }
            }
            
            // Track accumulated size
            rangeState.AccumulatedSize = currentOffset - rangeState.StartOffset
            
            // Check if should finalize range
            if rangeState.AccumulatedSize >= threshold {
                // Create range descriptor
                rangeDesc := RangeDescriptor{
                    From: rangeState.StartIndex,
                    To: currentIndex + 1,
                    Offset: rangeState.StartOffset,
                    Size: rangeState.AccumulatedSize,
                }
                indexBuilder.AddRangeDescriptor(rangeDesc)
                
                // Start new range
                rangeState = &RangeState{
                    StartIndex: currentIndex + 1,
                    StartOffset: currentOffset,
                }
            }
        }
    }
    
    // Finalize any remaining range
    if rangeState != nil {
        rangeDesc := RangeDescriptor{
            From: rangeState.StartIndex,
            To: enc.CurrentIndex() + 1,
            Offset: rangeState.StartOffset,
            Size: enc.Offset() - rangeState.StartOffset,
        }
        indexBuilder.AddRangeDescriptor(rangeDesc)
    }
    
    return nil
}
```

## Conclusion

**StreamEncoder/StreamDecoder provide**:
- ✅ All queryable state needed (depth, breadth, offsets)
- ✅ Explicit boundaries for structure tracking
- ✅ Offset tracking in new snapshot

**What we need**:
- ⚠️ External range state tracking (which elements, accumulated size)
- ⚠️ Range finalization logic (when to create range descriptor)

**No buffering or dual writing needed** - chunks are written once, sequentially, as we process them!

The impedance mismatch is solved - StreamEncoder/StreamDecoder provide everything needed for incremental index building with range descriptors.
