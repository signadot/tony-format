# Analysis: Bracketed-Only Streaming Simplification

## Your Two Points

### 1. Sparse Array vs Array vs Object

**Current Situation**:
- **Arrays**: `[1, 2, 3]` → `TLSquare`/`TRSquare` tokens
- **Objects**: `{key: value}` → `TLCurl`/`TRCurl` tokens  
- **Sparse Arrays**: `{0: val, 5: val}` → `TLCurl`/`TRCurl` tokens, but with numeric keys → tagged `!sparsearray`

**The Challenge**:
- Sparse arrays are **structurally objects** (use `{` `}`)
- But **semantically arrays** (numeric keys, tagged `!sparsearray`)
- At streaming time, we see `{` but don't know if it's object or sparse array until we see keys

**Options**:
1. **Treat as object in streaming layer**: Let higher layers (parsing) determine sparse array vs object based on key types
2. **Explicit sparse array methods**: `BeginSparseArray()` - but this requires lookahead or different API
3. **Tag-based**: `BeginObject()` with option to mark as sparse array later

**Recommendation**: **Option 1** - Treat as object in streaming layer. The distinction is semantic (key types), not structural. Higher layers can determine sparse array vs object.

### 2. Block Style Complexity

**Current Block Style Issues**:
- Uses `TArrayElt` token (the `-` in YAML-style lists)
- **No explicit boundaries** - relies on indentation
- **Path tracking doesn't work** - code explicitly says: "Block-style arrays don't have explicit boundaries, so we can't track them accurately"
- **Complex state machine** - need to track indentation levels, detect structure boundaries
- **Ambiguous parsing** - hard to know when structure ends

**Bracketed Style Benefits**:
- ✅ **Explicit boundaries**: `{` `}` `[` `]` clearly mark structure
- ✅ **Simple state machine**: Push on `{`/`[`, pop on `}`/`]`
- ✅ **Path tracking works**: Can track paths accurately
- ✅ **No indentation ambiguity**: Structure is explicit in tokens
- ✅ **Matches jsontext pattern**: Similar to JSON (all bracketed)

**Code Evidence**:
```go
// From token/sink.go line 382-385
case TArrayElt:
    // Block-style array element - skip path tracking
    // Path tracking only works for bracketed structures (TLSquare/TRSquare)
    // Block-style arrays don't have explicit boundaries, so we can't track them accurately
```

**Recommendation**: **YES, bracketed-only would be MUCH simpler**. Eliminates:
- ~100+ lines of TArrayElt handling
- Indentation-based structure detection
- Ambiguous boundary detection
- Complex path tracking edge cases

## Can We Get Rid of token.Source?

### Current Usage

**token.Source is used by**:
1. `parse.NodeParser` - for incremental parsing
2. Path tracking (internal to token.Source)

### Analysis

**What token.Source does**:
- Tokenizes from `io.Reader` (buffering, EOF handling)
- Tracks paths (for bracketed structures only)
- Maintains tokenization state

**What we need for bracketed-only streaming**:
- Tokenize from `io.Reader` ✅ (still needed)
- Track paths ✅ (but simpler - only bracketed)
- Maintain tokenization state ✅ (still needed)

### Options

**Option A: Keep token.Source, simplify it**
- Remove block style handling (TArrayElt)
- Simplify path tracking (only bracketed)
- Keep tokenization logic
- **Pros**: Reuse existing code, minimal changes
- **Cons**: Still have token.Source complexity

**Option B: Replace token.Source with simpler token reader**
- Create `token.Reader` - just tokenization, no path tracking
- Move path tracking to `parse.StreamDecoder`
- **Pros**: Cleaner separation, simpler token package
- **Cons**: More code changes, need to migrate NodeParser

**Option C: Eliminate token.Source entirely**
- Move tokenization into `parse.StreamDecoder`
- **Pros**: Single responsibility, no token.Source
- **Cons**: Loses separation, tokenization becomes part of parse package

### Recommendation

**Option B** - Replace with simpler token reader:

1. **Create `token.Reader`**: Just tokenization, no path tracking
   ```go
   type Reader struct {
       reader io.Reader
       // ... tokenization state only
   }
   func (r *Reader) Read() ([]Token, error)
   ```

2. **Move path tracking to `parse.StreamDecoder`**: 
   - StreamDecoder manages its own path stack
   - Uses token.Reader internally
   - Explicit control over path tracking

3. **Update NodeParser**: Use `token.Reader` instead of `token.Source`

**Benefits**:
- ✅ Simpler token package (just tokenization)
- ✅ Path tracking in parse package (where it's used)
- ✅ Can eliminate token.Source entirely
- ✅ Clear separation: tokenization vs. streaming

## Revised Design: Bracketed-Only Streaming

### Simplified API

```go
// parse.StreamEncoder - bracketed structures only
enc.BeginObject()      // { ... }
enc.BeginArray()       // [ ... ]
enc.EndObject()        // }
enc.EndArray()         // ]

// No block style methods needed!
```

### Simplified State Machine

```go
type stateMachine struct {
    depth int
    stack []structureType  // Just object or array
}

// Simple push/pop - no TArrayElt, no indentation tracking
func (sm *stateMachine) push(typ structureType) {
    sm.stack = append(sm.stack, typ)
    sm.depth++
}

func (sm *stateMachine) pop() {
    if len(sm.stack) > 0 {
        sm.stack = sm.stack[:len(sm.stack)-1]
        sm.depth--
    }
}
```

### Simplified Path Tracking

```go
// Only need to handle:
// - Object keys: "key" → "key.subkey"
// - Array indices: [0], [1], [2]
// - Sparse array indices: {0}, {5} (treated as object keys in streaming)

// No need for:
// - TArrayElt handling
// - Indentation-based structure detection
// - Ambiguous boundaries
```

## Impact Analysis

### What We Lose

1. **Block style support in streaming**:
   - Can't stream block-style documents
   - But: Can convert block → bracketed before streaming
   - Or: Use full parse for block style, streaming for bracketed

2. **token.Source**:
   - Remove entirely
   - Replace with simpler `token.Reader`

### What We Gain

1. **Simplicity**:
   - ~50% less code
   - Clearer state machine
   - Easier to understand and maintain

2. **Reliability**:
   - No ambiguous boundaries
   - Accurate path tracking
   - Predictable behavior

3. **Performance**:
   - Simpler state machine = faster
   - Less branching = better CPU cache

4. **Foundation**:
   - Clean API for streaming
   - Easy to extend
   - Better for indexing use cases

## Revised Migration Plan

### Phase 1: Simplify Token Package
1. Create `token.Reader` (just tokenization)
2. Remove path tracking from token package
3. Remove TArrayElt handling (if not needed elsewhere)

### Phase 2: Implement StreamEncoder/Decoder
1. Implement with bracketed-only support
2. Path tracking in parse package
3. Simple state machine

### Phase 3: Update NodeParser
1. Use `token.Reader` instead of `token.Source`
2. Test incremental parsing still works

### Phase 4: Remove token.Source
1. Delete `token/source.go`
2. Update all references

## Conclusion

**YES to both**:

1. ✅ **Bracketed-only streaming would be MUCH simpler**
   - Eliminates ~100+ lines of complexity
   - Makes path tracking reliable
   - Matches jsontext pattern

2. ✅ **We can get rid of token.Source**
   - Replace with simpler `token.Reader`
   - Move path tracking to `parse.StreamDecoder`
   - Cleaner separation of concerns

**Recommendation**: Proceed with bracketed-only streaming and eliminate token.Source.
