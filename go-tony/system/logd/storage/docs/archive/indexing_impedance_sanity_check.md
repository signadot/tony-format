# Sanity Check: Snapshot Indexing Impedance Match

## Snapshot Index Requirements

From `internal/snap/index.go`:
```go
type Index struct {
    StartField *string      // Field name where index starts
    Start      int          // Start position
    End        int          // End position  
    Offset     int64        // Byte offset in snapshot
    Size       int64        // Size in bytes
    
    ParentField       string   // Parent field name
    ParentSparseIndex int      // Parent sparse array index
    ParentIndex       int      // Parent array index
    Parent            *Index   // Parent index entry
    
    Children []Index           // Child index entries
}
```

## What We Need to Build This

### 1. Path Boundaries
- **Need**: Know when paths start and end
- **For**: `Start`, `End`, `Size` calculation

### 2. Byte Offsets
- **Need**: Track absolute byte offsets at boundaries
- **For**: `Offset` field

### 3. Field Names
- **Need**: Get object keys
- **For**: `StartField`, `ParentField`

### 4. Array Indices
- **Need**: Get array positions
- **For**: `ParentIndex`

### 5. Sparse Array Indices
- **Need**: Get sparse array positions
- **For**: `ParentSparseIndex`

### 6. Hierarchical Structure
- **Need**: Build parent-child relationships
- **For**: `Parent` pointer, `Children` array

## Old API: token.{Sink,Source}

### Building Index with TokenSink

```go
sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
    // Limited context:
    // - offset: byte offset ✅
    // - path: string (e.g., "key", "key[0]") ⚠️
    // - tok: token ⚠️
    
    // Problems:
    // ❌ Must parse path string to get StartField
    // ❌ Must infer parent from path string
    // ❌ No explicit end tracking (can't compute Size)
    // ❌ No depth information
    // ❌ No parent index reference
    // ❌ State is hidden (can't query)
    
    entry := &Index{
        Offset: int64(offset),
        // StartField: ??? (must parse path)
        // Parent: ??? (must infer from path)
        // Size: ??? (no end tracking)
    }
})
```

**Problems**:
- ❌ **Implicit state**: Can't query depth, parent, structure type
- ❌ **String parsing**: Must parse path to extract components
- ❌ **No end tracking**: Can't compute `Size` reliably
- ❌ **No parent context**: Must infer parent from path string
- ❌ **Callback-only**: Limited context, hard to build hierarchical structure

## New API: parse.StreamEncoder/StreamDecoder

### Building Index with StreamEncoder

```go
enc := parse.NewStreamEncoder(writer)
indexStack := []*Index{rootIndex}  // Track parent chain

enc.BeginObject()
// Full context available:
path := enc.CurrentPath()        // ""
offset := enc.Offset()           // 0
depth := enc.Depth()             // 1

enc.WriteKey("key")
// Full context available:
path = enc.CurrentPath()         // "key"
offset = enc.Offset()            // Byte offset at "key"
key := enc.CurrentKey()          // "key" (direct access!)
parentPath := enc.ParentPath()   // "" (direct access!)
parentIdx := indexStack[len(indexStack)-1]  // Explicit parent reference

// Build index entry with full context
entry := &Index{
    StartField: &key,              // ✅ Direct access
    Offset: offset,                 // ✅ Queryable
    Parent: parentIdx,              // ✅ Explicit reference
    ParentField: parentPath,        // ✅ Queryable
    Start: 0,                       // Can track token position if needed
}

enc.WriteString("value")
enc.EndObject()
// Can compute size at end boundary
endOffset := enc.Offset()
entry.Size = endOffset - entry.Offset  // ✅ Explicit size calculation
entry.End = ...  // Can track if needed
```

**Benefits**:
- ✅ **Explicit state**: Query depth, path, parent, structure type
- ✅ **Direct access**: `CurrentKey()`, `CurrentIndex()` give direct access
- ✅ **Explicit boundaries**: `Begin/End` pairs make boundaries clear
- ✅ **Parent context**: `ParentPath()` enables hierarchical building
- ✅ **Size calculation**: Can compute from offsets at boundaries

### Building Index with StreamDecoder

```go
dec := parse.NewStreamDecoder(reader)
indexStack := []*Index{rootIndex}

for {
    tok, err := dec.ReadToken()
    if err == io.EOF { break }
    
    switch tok.Type {
    case token.BeginObject, token.BeginArray:
        // Structure start - create index entry
        parentIdx := indexStack[len(indexStack)-1]
        
        entry := &Index{
            Offset: dec.Offset(),           // ✅ Queryable
            Parent: parentIdx,               // ✅ Explicit reference
            ParentField: dec.ParentPath(),   // ✅ Queryable
        }
        
        if dec.IsInObject() {
            key := dec.CurrentKey()         // ✅ Direct access
            entry.StartField = &key
            
            // Check if sparse array (numeric key)
            if sparseIdx, err := strconv.Atoi(key); err == nil {
                entry.ParentSparseIndex = sparseIdx  // ✅ Can derive
            }
        } else if dec.IsInArray() {
            idx := dec.CurrentIndex()       // ✅ Direct access
            entry.ParentIndex = idx
        }
        
        parentIdx.Children = append(parentIdx.Children, *entry)
        indexStack = append(indexStack, entry)
        
    case token.EndObject, token.EndArray:
        // Structure end - compute size
        currentIdx := indexStack[len(indexStack)-1]
        endOffset := dec.Offset()
        currentIdx.Size = endOffset - currentIdx.Offset  // ✅ Size calculation
        indexStack = indexStack[:len(indexStack)-1]
    }
}
```

**Benefits**:
- ✅ **Queryable state**: Can query at any time
- ✅ **Explicit boundaries**: Clear start/end markers
- ✅ **Parent tracking**: Stack maintains parent chain
- ✅ **Size calculation**: Can compute from offsets

## Requirement-by-Requirement Check

| Requirement | Old API | New API | Match |
|------------|---------|---------|-------|
| **Path Boundaries** | Callback at start, no end | Explicit Begin/End pairs | ✅ **SOLVED** |
| **Byte Offsets** | `Offset()` tied to writing | `Offset()` queryable | ✅ **SOLVED** |
| **Field Names** | Parse from path string | `CurrentKey()` direct | ✅ **SOLVED** |
| **Array Indices** | Parse from path string | `CurrentIndex()` direct | ✅ **SOLVED** |
| **Sparse Indices** | Parse from path string | Parse from `CurrentKey()` | ⚠️ **PARTIAL** (still parse, but easier) |
| **Hierarchical Structure** | Infer from path string | `ParentPath()` + stack | ✅ **SOLVED** |
| **Size Calculation** | Manual tracking | Explicit boundaries | ✅ **SOLVED** |
| **Parent References** | Hard to maintain | Stack makes it easy | ✅ **SOLVED** |

## Example: Complete Index Building

```go
func BuildIndex(reader io.Reader) (*Index, error) {
    dec := parse.NewStreamDecoder(reader)
    root := &Index{Offset: 0}
    stack := []*Index{root}
    
    for {
        tok, err := dec.ReadToken()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        
        switch tok.Type {
        case token.BeginObject:
            parent := stack[len(stack)-1]
            child := &Index{
                Offset: dec.Offset(),
                Parent: parent,
                ParentField: dec.ParentPath(),
            }
            parent.Children = append(parent.Children, *child)
            stack = append(stack, child)
            
        case token.BeginArray:
            parent := stack[len(stack)-1]
            child := &Index{
                Offset: dec.Offset(),
                Parent: parent,
                ParentField: dec.ParentPath(),
            }
            parent.Children = append(parent.Children, *child)
            stack = append(stack, child)
            
        case token.TColon:
            // Object key boundary
            if dec.IsInObject() {
                current := stack[len(stack)-1]
                key := dec.CurrentKey()
                current.StartField = &key
                
                // Handle sparse array
                if sparseIdx, err := strconv.Atoi(key); err == nil {
                    current.ParentSparseIndex = sparseIdx
                }
            }
            
        case token.EndObject, token.EndArray:
            current := stack[len(stack)-1]
            current.Size = dec.Offset() - current.Offset
            stack = stack[:len(stack)-1]
        }
    }
    
    return root, nil
}
```

**Key Points**:
- ✅ Explicit boundaries (`Begin/End`)
- ✅ Queryable state (`CurrentKey()`, `CurrentIndex()`, `Offset()`)
- ✅ Parent tracking via stack
- ✅ Size calculation from offsets
- ✅ Clean, understandable code

## Range Descriptors: The Missing Piece

### The Problem

**For large containers** (millions of elements), we can't index every element. We need **range descriptors** that handle breadth (sibling elements) as well as depth (nested structures).

**Current implementation** (`from_ir.go`):
- Accumulates `rangeValues` (full `ir.Node` structures)
- Encodes them to create chunk data
- Creates `!snap-range` nodes with `[from, to, offset, size]`

**Question**: Can StreamEncoder build range descriptors when streaming (without full `ir.Node` structures)?

### What Range Descriptors Need

**Range descriptor structure**:
```go
!snap-range(from, to) [offset, size]
```

**Requirements**:
1. ✅ **Range boundaries**: `from`, `to` (element indices)
2. ✅ **Chunk offset**: Where chunk starts in snapshot
3. ✅ **Chunk size**: Size of encoded chunk data
4. ⚠️ **Chunk data**: Need to encode elements to create chunk

**The Challenge**: 
- When streaming, we write elements directly to snapshot
- But range descriptors need chunk data (encoded elements)
- We can't read back from snapshot easily (no random access)

### Solution: Buffer Range Data

**During encoding**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeState := &RangeState{
    StartOffset: enc.Offset(),
    StartIndex: 0,
    AccumulatedSize: 0,
    ElementCount: 0,
    Buffer: &bytes.Buffer{},  // Buffer for chunk data
    RangeEncoder: parse.NewStreamEncoder(rangeState.Buffer),  // Encode to buffer
}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    // Write to snapshot
    enc.WriteInt(i)
    
    // Also write to range buffer
    rangeState.RangeEncoder.WriteInt(i)
    
    size := enc.Offset() - offsetBefore
    rangeState.AccumulatedSize += size
    rangeState.ElementCount++
    
    if rangeState.AccumulatedSize >= threshold {
        // Finalize range
        chunkData := rangeState.Buffer.Bytes()
        chunkOffset := writeChunk(chunkData)  // Write buffered data
        
        rangeNode := &ir.Node{
            Tag: fmt.Sprintf("!snap-range(%d,%d)", 
                rangeState.StartIndex, 
                rangeState.StartIndex + rangeState.ElementCount),
            Values: []*ir.Node{
                {Type: ir.NumberType, Int64: &chunkOffset},
                {Type: ir.NumberType, Int64: &rangeState.AccumulatedSize},
            },
        }
        
        indexNode.Values = append(indexNode.Values, rangeNode)
        
        // Reset range state
        rangeState = &RangeState{
            StartOffset: enc.Offset(),
            StartIndex: i + 1,
            Buffer: &bytes.Buffer{},
            RangeEncoder: parse.NewStreamEncoder(rangeState.Buffer),
        }
    }
}
```

**Key Points**:
- ✅ **Dual encoding**: Write to snapshot AND buffer
- ✅ **Range state**: Track boundaries, size, element count
- ✅ **Chunk creation**: Buffer provides chunk data
- ✅ **Index building**: Build `ir.Node` index structure

### Alternative: Single-Pass with Tee Writer

**More efficient approach**:
```go
rangeBuffer := &bytes.Buffer{}
teeWriter := io.MultiWriter(snapshotWriter, rangeBuffer)
enc := parse.NewStreamEncoder(teeWriter)

// Now writes go to both snapshot and buffer simultaneously
enc.WriteInt(42)  // Written to both!
```

**But**: This writes everything twice, not just ranges.

### Better: Conditional Buffering

**Only buffer when building range**:
```go
enc := parse.NewStreamEncoder(snapshotWriter)
rangeState := &RangeState{...}

enc.BeginArray()
for i := 0; i < 1000000; i++ {
    offsetBefore := enc.Offset()
    
    if rangeState.IsBuildingRange {
        // Write to both snapshot and buffer
        enc.WriteInt(i)
        rangeState.Buffer.Write(enc.LastWriteBytes())  // Need encoder to provide this
    } else {
        // Write only to snapshot
        enc.WriteInt(i)
    }
    
    size := enc.Offset() - offsetBefore
    
    if size < threshold {
        // Small element - add to range
        if !rangeState.IsBuildingRange {
            rangeState.StartRange(enc.Offset())
        }
        rangeState.AccumulatedSize += size
        // ... buffer logic ...
    } else {
        // Large element - index individually
        if rangeState.IsBuildingRange {
            rangeState.FinalizeRange()
        }
        addIndexEntry(i, offsetBefore, size)
    }
}
```

**Requirement**: StreamEncoder needs to provide `LastWriteBytes()` or similar.

### Verdict on Range Descriptors

**StreamEncoder CAN support range descriptors**, but requires:

1. ✅ **Explicit boundaries**: `Begin/End` pairs (StreamEncoder provides)
2. ✅ **Queryable offsets**: `Offset()` (StreamEncoder provides)
3. ✅ **Range state tracking**: External state (we provide)
4. ⚠️ **Chunk data**: Need to buffer encoded data (requires additional API or dual encoding)

**Missing API**:
- ⚠️ `LastWriteBytes()` - to capture what was written
- ⚠️ Or: `WriteToBuffer()` - to write to buffer instead of writer
- ⚠️ Or: Dual encoding approach (write to both snapshot and buffer)

**Conclusion**: 
- ✅ StreamEncoder provides foundation (boundaries, offsets, state)
- ⚠️ Range descriptors need additional buffering mechanism
- ⚠️ May need to extend StreamEncoder API or use dual encoding

## Verdict

### Impedance Match: ✅ **SOLVED** (with caveat)

**The new API solves the impedance mismatch**:

1. ✅ **Explicit state**: All requirements queryable
2. ✅ **Explicit boundaries**: Clear start/end markers
3. ✅ **Direct access**: No string parsing needed (except sparse indices)
4. ✅ **Parent context**: Enables hierarchical building
5. ✅ **Size calculation**: Explicit boundaries + offsets

**For range descriptors**:
- ✅ **Foundation**: StreamEncoder provides boundaries, offsets, state
- ⚠️ **Buffering**: Need mechanism to capture chunk data (may need API extension)

**Remaining issues**:
- ⚠️ Sparse array index parsing (still need `strconv.Atoi()`, but it's straightforward)
- ⚠️ Range descriptor chunk buffering (need to capture encoded data)

**Conclusion**: ✅ **The new API is a good match for snapshot indexing!**

The impedance mismatch is solved. The new API provides everything needed to build snapshot indexes efficiently and cleanly. Range descriptors may require additional buffering mechanism, but the foundation is solid.
