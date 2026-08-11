# Sanity Check: Snapshot Indexing Impedance Match

## Question

Does the new `parse.StreamEncoder/StreamDecoder` API actually solve the impedance mismatch for snapshot indexing?

## Snapshot Indexing Requirements

From `internal/snap/index.go`:
```go
type Index struct {
    StartField *string      // Field name where index starts
    Start      int          // Start position (token/byte)
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

**Key Requirements**:
1. ✅ **Path boundaries**: Know when paths start/end
2. ✅ **Byte offsets**: Track absolute byte offsets at boundaries
3. ✅ **Hierarchical structure**: Build parent-child relationships
4. ✅ **Field names**: Track object keys (for `StartField`, `ParentField`)
5. ✅ **Array indices**: Track array positions (for `ParentIndex`)
6. ✅ **Sparse array indices**: Track sparse array positions (for `ParentSparseIndex`)
7. ✅ **Size calculation**: Compute sizes between boundaries

## What token.{Sink,Source} Provided (Old)

### TokenSink
```go
sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
    // Callback fires at node start
    // But:
    // - No explicit end tracking
    // - No parent context queryable
    // - Path is string (must parse to get components)
    // - No depth queryable
    // - State is hidden
})
```

**Problems**:
- ❌ **Implicit state**: Can't query depth, parent path, structure type
- ❌ **Callback-only**: Must build index in callback (limited context)
- ❌ **No end tracking**: Don't know when paths end (hard to compute size)
- ❌ **String parsing**: Path is string, must parse to get field/index
- ❌ **No parent access**: Can't easily get parent index entry

### TokenSource
```go
source := token.NewTokenSource(reader)
tokens, err := source.Read()
path := source.CurrentPath()  // Can query, but...
// Problems:
// - State updated implicitly during Read()
// - Can't pause/resume without consuming tokens
// - No explicit control over when indexing happens
```

## What parse.StreamEncoder/StreamDecoder Provides (New)

### StreamEncoder
```go
enc := parse.NewStreamEncoder(writer)

enc.BeginObject()
path := enc.CurrentPath()        // "" (root)
offset := enc.Offset()            // 0

enc.WriteKey("key")
path = enc.CurrentPath()          // "key"
offset = enc.Offset()             // Byte offset at "key"
depth := enc.Depth()               // 1
parentPath := enc.ParentPath()    // ""

// Build index entry
indexEntry := &Index{
    StartField: &enc.CurrentKey(),  // "key"
    Offset: offset,
    Parent: parentIndex,              // Can track explicitly
}

enc.WriteString("value")
enc.EndObject()
```

**Benefits**:
- ✅ **Explicit state**: Query depth, path, parent path, structure type
- ✅ **Explicit boundaries**: `BeginObject/Array` → `EndObject/Array` pairs
- ✅ **Parent context**: `ParentPath()` gives parent path
- ✅ **Field access**: `CurrentKey()` gives object key directly
- ✅ **Index access**: `CurrentIndex()` gives array index directly
- ✅ **Offset tracking**: `Offset()` gives byte offset at any time
- ✅ **Size calculation**: Can compute from offsets at boundaries

### StreamDecoder
```go
dec := parse.NewStreamDecoder(reader)

for {
    tok, err := dec.ReadToken()
    if err == io.EOF { break }
    
    // Query state at any time
    path := dec.CurrentPath()      // e.g., "key", "key[0]"
    depth := dec.Depth()            // Current nesting depth
    offset := dec.Offset()          // Byte offset
    parentPath := dec.ParentPath()  // Parent path
    
    // Build index at boundaries
    if dec.IsInObject() && tok.Type == token.TColon {
        // Object key boundary
        key := dec.CurrentKey()
        parentIdx := getParentIndex(parentPath)  // Can look up parent
        
        indexEntry := &Index{
            StartField: &key,
            Offset: offset,
            Parent: parentIdx,  // Explicit parent reference
            ParentField: dec.ParentPath(),  // Can derive
        }
        addToIndex(indexEntry)
    }
    
    if tok.Type == token.EndObject || tok.Type == token.EndArray {
        // Path boundary end - can compute size
        endOffset := dec.Offset()
        // Update index entry with size
        updateIndexSize(path, endOffset)
    }
}
```

**Benefits**:
- ✅ **Queryable state**: Can query depth, path, parent at any time
- ✅ **Explicit control**: Can build index at specific boundaries
- ✅ **Parent tracking**: Can maintain parent index references
- ✅ **Size calculation**: Can compute sizes from offsets

## Impedance Match Analysis

### Requirement 1: Path Boundaries ✅

**Old**: Callback fires at node start, but no explicit end tracking
**New**: Explicit `BeginObject/Array` → `EndObject/Array` pairs

**Match**: ✅ **SOLVED** - Explicit boundaries make tracking reliable

### Requirement 2: Byte Offsets ✅

**Old**: `TokenSink.Offset()` exists but tied to token writing
**New**: `StreamEncoder.Offset()` and `StreamDecoder.Offset()` queryable at any time

**Match**: ✅ **SOLVED** - Offsets are queryable, separate from token stream

### Requirement 3: Hierarchical Structure ✅

**Old**: Must infer parent from path string (`"a.b"` → parent is `"a"`)
**New**: `ParentPath()` gives parent path, can maintain parent index references

**Match**: ✅ **SOLVED** - Explicit parent context enables hierarchical building

### Requirement 4: Field Names ✅

**Old**: Path is string, must parse to extract field name
**New**: `CurrentKey()` gives object key directly

**Match**: ✅ **SOLVED** - Direct access to field names

### Requirement 5: Array Indices ✅

**Old**: Path is string, must parse to extract array index
**New**: `CurrentIndex()` gives array index directly

**Match**: ✅ **SOLVED** - Direct access to array indices

### Requirement 6: Sparse Array Indices ✅

**Old**: Path is string, must parse to extract sparse index (`"key{5}"`)
**New**: `CurrentKey()` gives sparse array key (as string), can parse if needed

**Match**: ⚠️ **PARTIAL** - Still need to parse sparse index from key string
- But: Sparse arrays use `BeginObject()` in streaming layer
- Key is the sparse index (as string)
- Can parse `{index}` from path if needed

### Requirement 7: Size Calculation ✅

**Old**: Must track offsets manually in callbacks, compute sizes
**New**: Can query offsets at boundaries, compute sizes explicitly

**Match**: ✅ **SOLVED** - Explicit boundaries + queryable offsets enable size calculation

## Example: Building Index with New API

```go
func BuildSnapshotIndex(reader io.Reader) (*Index, error) {
    dec := parse.NewStreamDecoder(reader)
    rootIndex := &Index{Offset: 0}
    indexStack := []*Index{rootIndex}  // Track parent chain
    
    for {
        tok, err := dec.ReadToken()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        
        // Track structure boundaries
        switch tok.Type {
        case token.BeginObject, token.BeginArray:
            // Structure start - create index entry
            parentIdx := indexStack[len(indexStack)-1]
            childIdx := &Index{
                Offset: dec.Offset(),
                Parent: parentIdx,
                ParentField: dec.ParentPath(),
            }
            
            if dec.IsInObject() {
                key := dec.CurrentKey()
                childIdx.StartField = &key
            } else if dec.IsInArray() {
                idx := dec.CurrentIndex()
                childIdx.ParentIndex = idx
            }
            
            parentIdx.Children = append(parentIdx.Children, *childIdx)
            indexStack = append(indexStack, childIdx)
            
        case token.EndObject, token.EndArray:
            // Structure end - compute size
            currentIdx := indexStack[len(indexStack)-1]
            endOffset := dec.Offset()
            currentIdx.Size = endOffset - currentIdx.Offset
            
            // Pop stack
            indexStack = indexStack[:len(indexStack)-1]
        }
    }
    
    return rootIndex, nil
}
```

**Key Benefits**:
- ✅ Explicit parent tracking via `indexStack`
- ✅ Queryable state (`IsInObject()`, `CurrentKey()`, `CurrentIndex()`)
- ✅ Explicit boundaries (`BeginObject/Array` → `EndObject/Array`)
- ✅ Size calculation from offsets
- ✅ Clean, understandable code

## Comparison: Old vs New

### Building Index Entry

**Old (TokenSink callback)**:
```go
sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
    // Limited context - just offset, path string, token
    // Must parse path to get components
    // No parent context
    // No depth information
    // No explicit end tracking
    
    // Build index entry (limited info)
    entry := &Index{
        // Must parse path string to get StartField
        // Must infer parent from path string
        // Can't compute size (no end tracking)
    }
})
```

**New (StreamEncoder)**:
```go
enc := parse.NewStreamEncoder(writer)

enc.BeginObject()
enc.WriteKey("key")
// Full context available
entry := &Index{
    StartField: &enc.CurrentKey(),      // Direct access
    Offset: enc.Offset(),                // Queryable
    Parent: parentIndex,                  // Explicit reference
    ParentField: enc.ParentPath(),       // Queryable
    // Can compute size at EndObject
}
enc.WriteString("value")
enc.EndObject()
// Can compute size here
entry.Size = enc.Offset() - entry.Offset
```

**Match**: ✅ **MUCH BETTER** - Full context, explicit control

## Remaining Issues?

### 1. Sparse Array Index Parsing

**Issue**: Sparse arrays use `BeginObject()`, key is string (e.g., `"5"`), but Index needs `ParentSparseIndex int`

**Solution**:
```go
if dec.IsInObject() {
    key := dec.CurrentKey()
    // Check if this is a sparse array (numeric key)
    if sparseIdx, err := strconv.Atoi(key); err == nil {
        // It's a sparse array index
        indexEntry.ParentSparseIndex = sparseIdx
    } else {
        // Regular object key
        indexEntry.StartField = &key
    }
}
```

**Status**: ✅ **SOLVABLE** - Can parse sparse index from key string

### 2. Size Calculation Timing

**Issue**: Need to know when to compute size (at `EndObject/Array`)

**Solution**: Explicit `EndObject/Array` calls make this clear

**Status**: ✅ **SOLVED** - Explicit boundaries enable size calculation

### 3. Parent Index References

**Issue**: Need to maintain parent index references for hierarchical structure

**Solution**: Use stack to track parent chain (shown in example above)

**Status**: ✅ **SOLVED** - Explicit parent tracking via stack

## Conclusion

### Impedance Match: ✅ **SOLVED**

**The new API solves the impedance mismatch**:

1. ✅ **Explicit state**: All state queryable (depth, path, parent, structure type)
2. ✅ **Explicit boundaries**: `Begin/End` pairs make boundaries clear
3. ✅ **Parent context**: `ParentPath()` enables hierarchical building
4. ✅ **Direct access**: `CurrentKey()`, `CurrentIndex()` give direct access
5. ✅ **Offset tracking**: Queryable offsets enable size calculation
6. ✅ **Control**: Can build index at specific boundaries with full context

**Remaining minor issues**:
- ⚠️ Sparse array index parsing (solvable, ~5 lines)
- ✅ Everything else is solved

**Verdict**: ✅ **The new API is a good match for snapshot indexing requirements!**
