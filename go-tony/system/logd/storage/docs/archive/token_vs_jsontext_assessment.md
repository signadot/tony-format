# Assessment: token.{Sink,Source} vs jsontext.{Encoder,Decoder} for Snapshot Indexing

## Executive Summary

After reviewing `jsontext.{Encoder,Decoder}`, `token.{Sink,Source}`, and the snapshot indexing requirements, **explicit stack management similar to jsontext would significantly benefit snapshot indexing**. The current `token.{Sink,Source}` interfaces have an impedance mismatch for indexing use cases because path tracking is implicit and embedded within token stream processing, making it difficult to:

1. **Query current state** (depth, path, parent context) without side effects
2. **Control when indexing happens** (need callbacks at specific boundaries)
3. **Build hierarchical index structures** (need explicit parent-child relationships)
4. **Handle partial reads** (need to know structure boundaries without consuming tokens)

## Current State: token.{Sink,Source}

### Architecture

**TokenSink** (`token/sink.go`):
- Writes tokens to `io.Writer`
- **Implicit path tracking**: Maintains `currentPath`, `pathStack`, `bracketStack` internally
- **Callback-based indexing**: `NodeOffsetCallback` called when nodes start
- **Hidden state**: Path tracking happens automatically in `updatePath()` during `Write()`
- **No queryable API**: Cannot inspect current depth, path, or stack state without side effects

**TokenSource** (`token/source.go`):
- Reads tokens from `io.Reader`
- **Implicit path tracking**: Same internal state as TokenSink
- **Read-only access**: `CurrentPath()` and `Depth()` methods exist but state is updated implicitly
- **No control**: Path tracking happens automatically during `Read()`

### Key Limitations for Indexing

1. **Implicit State Management**:
   ```go
   // Current approach: Path tracking happens automatically
   ts.Write(tokens)  // updatePath() called internally, state mutated
   path := ts.currentPath  // Can't access - private field
   ```

2. **Callback-Only Indexing**:
   ```go
   // Must use callback to capture indexing events
   onNodeStart := func(offset int, path string, token Token) {
       // Build index here - but limited context
   }
   sink := NewTokenSink(w, onNodeStart)
   ```

3. **No Structural Queries**:
   - Cannot query: "What is the current depth?"
   - Cannot query: "What is the parent path?"
   - Cannot query: "What structure am I in?" (object vs array)
   - Cannot pause/resume indexing without consuming tokens

4. **Tight Coupling**:
   - Path tracking logic (`updatePath()`) is 300+ lines of complex state machine
   - Embedded in token stream processing
   - Hard to reuse for different indexing strategies

## Proposed: jsontext-Style Explicit Stack Management

### Architecture

**jsontext.Encoder** (`encoding/json/jsontext/encode.go`):
- **Explicit state machine**: `stateMachine` (via `Tokens`) with `Depth()`, `Push()`, `Pop()`
- **Separate stacks**: `objectNameStack`, `objectNamespaceStack`
- **Queryable state**: Can inspect current depth, last token, structure type
- **Control flow**: Explicit `WriteToken()` calls, state updates are visible

**jsontext.Decoder** (`encoding/json/jsontext/decode.go`):
- **Explicit state tracking**: `stateMachine` tracks structure depth
- **Queryable offsets**: `offsetAt()`, `previousOffsetStart()`, `previousOffsetEnd()`
- **Controlled reading**: `ReadToken()` explicitly updates state
- **Buffer management**: Explicit buffer segments for read/unread portions

### Key Benefits for Indexing

1. **Explicit Stack Management**:
   ```go
   // Proposed approach: Explicit, queryable state
   type IndexEncoder struct {
       tokens stateMachine  // Explicit depth tracking
       paths  pathStack     // Explicit path stack
       names  nameStack     // Object key stack
   }
   
   func (e *IndexEncoder) Depth() int {
       return e.tokens.Depth()  // Queryable without side effects
   }
   
   func (e *IndexEncoder) CurrentPath() string {
       return e.paths.Current()  // Queryable
   }
   
   func (e *IndexEncoder) ParentPath() string {
       return e.paths.Parent()  // Queryable
   }
   ```

2. **Controlled Indexing**:
   ```go
   // Can build index at specific boundaries
   func (e *IndexEncoder) WriteToken(tok Token) error {
       // Update state explicitly
       e.tokens.Push(tok)
       e.paths.Update(tok)
       
       // Index at boundaries
       if e.isIndexBoundary(tok) {
           idx := &Index{
               StartField: e.names.Current(),
               Start: e.tokens.Depth(),
               Offset: e.offset(),
               Parent: e.paths.ParentIndex(),
           }
           e.index.Add(idx)
       }
       
       return e.writer.Write(tok)
   }
   ```

3. **Hierarchical Index Building**:
   ```go
   // Can build parent-child relationships explicitly
   func (e *IndexEncoder) BeginObject() error {
       parentIdx := e.currentIndex()  // Get current index entry
       e.tokens.Push(BeginObject)
       
       // Create child index entry
       childIdx := &Index{
           Parent: parentIdx,
           ParentField: e.names.Current(),
       }
       e.indexStack.Push(childIdx)
   }
   ```

4. **Partial Structure Navigation**:
   ```go
   // Can query structure without consuming tokens
   func (idx *Index) FindPath(path string) (*Index, error) {
       // Navigate index structure directly
       // No need to parse tokens
       return idx.navigate(path)
   }
   ```

## Snapshot Index Requirements

From `internal/snap/index.go` and design docs:

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

### Requirements Analysis

1. **Path Boundaries**: Need to track when paths start/end
   - **Current**: Callback fires at node start, but no explicit end tracking
   - **jsontext-style**: Explicit `BeginObject/Array` → `EndObject/Array` pairs

2. **Hierarchical Structure**: Need parent-child relationships
   - **Current**: Must infer from path strings (`"a.b"` → parent is `"a"`)
   - **jsontext-style**: Explicit stack maintains parent context

3. **Offset Tracking**: Need byte offsets at boundaries
   - **Current**: `TokenSink.Offset()` exists but tied to token writing
   - **jsontext-style**: Explicit `offsetAt()` methods, separate from token stream

4. **Size Calculation**: Need to compute sizes between boundaries
   - **Current**: Must track offsets manually in callbacks
   - **jsontext-style**: Can compute from explicit boundary markers

5. **Partial Reads**: Need to read specific paths without full parse
   - **Current**: Must parse entire stream, use callbacks to skip
   - **jsontext-style**: Can navigate index structure, then read only needed portions

## Comparison Matrix

| Feature | token.{Sink,Source} | jsontext-style | Benefit for Indexing |
|---------|---------------------|----------------|---------------------|
| **Stack Depth** | Implicit (private) | Explicit (`Depth()`) | ✅ Queryable |
| **Current Path** | Implicit (private) | Explicit (`CurrentPath()`) | ✅ Queryable |
| **Parent Context** | Inferred from path | Explicit (`Parent()`) | ✅ Direct access |
| **Structure Type** | Inferred from tokens | Explicit (`isObject()`, `isArray()`) | ✅ Type-safe |
| **Offset Tracking** | Tied to token stream | Separate (`offsetAt()`) | ✅ Flexible |
| **Boundary Control** | Callback-only | Explicit (`Begin/End` pairs) | ✅ Precise control |
| **State Queries** | None (private) | Queryable methods | ✅ Inspect without side effects |
| **Partial Processing** | Must consume tokens | Can navigate index | ✅ Efficient |

## Recommended Approach

### Option 1: Extend token.{Sink,Source} with Queryable API (Minimal Change)

**Pros**:
- Minimal disruption to existing code
- Can add methods incrementally

**Cons**:
- Still has implicit state management
- Path tracking logic remains embedded
- Doesn't solve fundamental impedance mismatch

**Implementation**:
```go
type TokenSink struct {
    // ... existing fields ...
}

// Add queryable methods
func (ts *TokenSink) Depth() int { return ts.depth }
func (ts *TokenSink) CurrentPath() string { return ts.currentPath }
func (ts *TokenSink) ParentPath() string {
    if len(ts.pathStack) > 0 {
        return ts.pathStack[len(ts.pathStack)-1]
    }
    return ""
}
func (ts *TokenSink) IsInObject() bool {
    return len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl
}
```

### Option 2: New IndexEncoder/IndexDecoder with Explicit Stack (Recommended)

**Pros**:
- Clean separation of concerns
- Explicit, queryable state
- Better suited for indexing use cases
- Can reuse token.{Sink,Source} for non-indexing scenarios

**Cons**:
- Requires new API
- More code to maintain
- Need to decide on relationship with token package

**Implementation**:
```go
package snap

import "github.com/signadot/tony-format/go-tony/token"

// IndexEncoder provides explicit stack management for building snapshot indexes
type IndexEncoder struct {
    writer io.Writer
    
    // Explicit state management
    depth      int           // Current nesting depth
    pathStack  []string      // Stack of paths
    nameStack  []string      // Stack of object keys
    indexStack []*Index      // Stack of index entries being built
    
    // Offset tracking
    offset int64
    
    // Current index being built
    currentIndex *Index
    rootIndex    *Index
}

func (e *IndexEncoder) WriteToken(tok token.Token) error {
    // Update explicit state
    e.updateState(tok)
    
    // Build index at boundaries
    if e.isIndexBoundary(tok) {
        e.addIndexEntry(tok)
    }
    
    // Write token
    return e.writeToken(tok)
}

func (e *IndexEncoder) Depth() int { return e.depth }
func (e *IndexEncoder) CurrentPath() string {
    if len(e.pathStack) > 0 {
        return e.pathStack[len(e.pathStack)-1]
    }
    return ""
}
func (e *IndexEncoder) ParentIndex() *Index {
    if len(e.indexStack) > 0 {
        return e.indexStack[len(e.indexStack)-1]
    }
    return nil
}
```

### Option 3: Hybrid - Use token.{Sink,Source} but Build Index Separately

**Pros**:
- Keep existing token infrastructure
- Build index from token stream + explicit tracking

**Cons**:
- Still has impedance mismatch
- Duplicate path tracking logic
- Less efficient

## Conclusion

**Recommendation**: **Option 2 - New IndexEncoder/IndexDecoder with Explicit Stack Management**

The snapshot indexing requirements confirm that `token.{Sink,Source}` has an unnecessary impedance mismatch for indexing use cases. The new `Index` struct needs:

1. ✅ **Explicit parent-child relationships** - jsontext-style stacks provide this
2. ✅ **Queryable state** - jsontext-style API provides this
3. ✅ **Boundary control** - jsontext-style Begin/End pairs provide this
4. ✅ **Offset tracking** - jsontext-style separate offset methods provide this
5. ✅ **Hierarchical navigation** - jsontext-style explicit stacks enable this

**Key Insight**: The `token.{Sink,Source}` interfaces are optimized for **translating token streams to/from readers/writers**. Snapshot indexing needs **explicit structural navigation and state queries**, which aligns better with jsontext's design philosophy.

**Migration Path**:
1. Create `IndexEncoder`/`IndexDecoder` in `internal/snap/` package
2. Use `token` package for tokenization (keep existing)
3. Build index using explicit stack management (new)
4. Keep `token.{Sink,Source}` for non-indexing use cases

This provides the best of both worlds: efficient token stream processing for general use, and explicit stack management for indexing use cases.
