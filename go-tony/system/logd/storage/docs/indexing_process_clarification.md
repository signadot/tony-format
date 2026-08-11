# Indexing Process Clarification

## User's Clarification

> "Indexing will consist of reading a snapshot by chunk and coordinating that with incremental construction of the next index and snapshot. Input will be existing index (in-memory) and sequence of chunks. Output is a new sequence of chunks, possibly aligned differently, and a corresponding new index. The new sequence of chunks will be written once, then the new index. There is no idea of 'write to snapshot OR buffer' in all that that I see."

## Correct Understanding

### The Process

**Input**:
- Existing index (in-memory `ir.Node`)
- Sequence of chunks (from existing snapshot file)

**Process**:
1. Read chunks from existing snapshot (using `StreamDecoder`)
2. Process chunks (apply patches, transform, etc.)
3. Build new chunks incrementally (using `StreamEncoder`)
4. Build new index incrementally (tracking offsets, ranges, etc.)

**Output**:
- New sequence of chunks (written once, sequentially)
- New index (written after chunks)

### The Flow

```
Existing Snapshot:
  [index][chunk1][chunk2][chunk3]...

Indexing Process:
  1. Read index (in-memory)
  2. For each chunk:
     - Read chunk with StreamDecoder
     - Process (apply patches, transform)
     - Write to new snapshot with StreamEncoder
     - Track offsets, build index entries
  3. Write new chunks sequentially
  4. Write new index

New Snapshot:
  [new index][new chunk1][new chunk2][new chunk3]...
```

## What This Means for StreamEncoder/StreamDecoder

### StreamDecoder Role

**Reads chunks from existing snapshot**:
```go
dec := parse.NewStreamDecoder(chunkReader)
// Read tokens from chunk
tok, err := dec.ReadToken()
// Query state: depth, path, offset (within chunk)
depth := dec.Depth()
path := dec.CurrentPath()
```

**Purpose**: 
- Read existing chunks
- Provide queryable state (depth, path, offset)
- Enable incremental processing

### StreamEncoder Role

**Writes chunks to new snapshot**:
```go
enc := parse.NewStreamEncoder(chunkWriter)
// Write tokens to new chunk
enc.WriteToken(tok)
// Query state: depth, path, offset (in new snapshot)
depth := enc.Depth()
path := enc.CurrentPath()
offset := enc.Offset()  // Tracks position in new snapshot
```

**Purpose**:
- Write new chunks sequentially
- Provide queryable state (depth, path, offset)
- Enable incremental index building

## The Range Descriptor Question

**Original Question**: 
> "When a container contains more elements than can be fit in memory, we cannot store an index entry for each element -- there's needs to be a way to handle state across the breadth of a container as well as the depth."

**What this means**:
- When reading a large container chunk with StreamDecoder
- We need to track breadth (which elements are in current range)
- We need to track depth (nested structures)
- When writing with StreamEncoder, we need to decide when to finalize ranges

### The Actual Flow

```go
// Read existing chunk
dec := parse.NewStreamDecoder(chunkReader)
indexBuilder := NewIndexBuilder()

dec.BeginArray()  // Large array container
for {
    tok, err := dec.ReadToken()
    if err == io.EOF { break }
    
    // Track breadth: which elements are in current range?
    // Track depth: nested structures?
    
    // Process token (apply patches, transform)
    processedTok := processToken(tok)
    
    // Write to new snapshot
    enc.WriteToken(processedTok)
    
    // Track for index building
    if shouldCreateRangeDescriptor() {
        // We've accumulated enough elements
        // Create range descriptor: [from, to, offset, size]
        rangeDesc := createRangeDescriptor(
            from: rangeStartIndex,
            to: currentIndex,
            offset: rangeStartOffset,
            size: enc.Offset() - rangeStartOffset,
        )
        indexBuilder.AddRangeDescriptor(rangeDesc)
    }
}

// Write new chunks sequentially (already done above)
// Write new index
indexNode := indexBuilder.Build()
writeIndex(indexWriter, indexNode)
```

## What StreamEncoder/StreamDecoder Need to Provide

### For Index Building

**From StreamDecoder**:
- ✅ Queryable state: `Depth()`, `CurrentPath()`, `CurrentKey()`, `CurrentIndex()`
- ✅ Offset tracking: `Offset()` (within chunk being read)

**From StreamEncoder**:
- ✅ Queryable state: `Depth()`, `CurrentPath()`, `CurrentKey()`, `CurrentIndex()`
- ✅ Offset tracking: `Offset()` (in new snapshot being written)
- ✅ Explicit boundaries: `BeginObject()`, `EndObject()`, `BeginArray()`, `EndArray()`

### For Range Descriptors

**What we need**:
1. ✅ **Track breadth**: Know which elements are siblings (same depth, sequential)
2. ✅ **Track depth**: Know nested structure
3. ✅ **Track offsets**: Know where ranges start/end in new snapshot
4. ✅ **Track sizes**: Know size of accumulated range

**What StreamEncoder/StreamDecoder provide**:
- ✅ **Breadth**: `CurrentIndex()` for arrays, `CurrentKey()` for objects
- ✅ **Depth**: `Depth()`, explicit `Begin/End` boundaries
- ✅ **Offsets**: `Offset()` tracks position in new snapshot
- ✅ **Sizes**: Can calculate from offset differences

## The Confusion I Had

**What I thought**:
- Building index DURING encoding of NEW snapshot
- "Write to snapshot OR buffer" for range building
- Dual writing or buffering

**What's actually happening**:
- **Reading** existing chunks with StreamDecoder
- **Processing** (transform, apply patches)
- **Writing** new chunks with StreamEncoder (write once, sequentially)
- **Building index** incrementally as we write

**No buffering needed** - chunks are written once, sequentially, as we process them.

## The Real Question

**User's question**: 
> "Are we sure index building only requires index entries and not full ir.Node's? When a container contains more elements than can be fit in memory, we cannot store an index entry for each element -- there's needs to be a way to handle state across the breadth of a container as well as the depth."

**What this means**:
- When processing a large container (millions of elements)
- We can't create an Index entry for each element
- We need range descriptors that group elements
- **Question**: Does StreamEncoder/StreamDecoder provide enough state to build range descriptors?

**Answer**: ✅ **YES** - StreamEncoder/StreamDecoder provide:
- ✅ Breadth tracking: `CurrentIndex()`, `CurrentKey()`
- ✅ Depth tracking: `Depth()`, explicit boundaries
- ✅ Offset tracking: `Offset()` in new snapshot
- ✅ Size calculation: Offset differences

**What we need to add**:
- ⚠️ **Range state tracking**: External state to track current range (start index, accumulated size, etc.)
- ⚠️ **Range finalization logic**: When to create range descriptor

But the **foundation is there** - StreamEncoder/StreamDecoder provide all the queryable state needed!

## Conclusion

**The indexing process**:
1. Read chunks with StreamDecoder
2. Process and write new chunks with StreamEncoder (write once, sequentially)
3. Build index incrementally as we write
4. Write new index after chunks

**StreamEncoder/StreamDecoder provide**:
- ✅ All queryable state needed (depth, breadth, offsets)
- ✅ Explicit boundaries for structure tracking
- ✅ Offset tracking in new snapshot

**What we need**:
- ⚠️ External range state tracking (which elements, accumulated size)
- ⚠️ Range finalization logic (when to create range descriptor)

**No buffering or dual writing needed** - chunks are written once, sequentially, as we process them!
