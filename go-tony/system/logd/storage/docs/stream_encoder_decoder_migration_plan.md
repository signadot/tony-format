# Migration Plan: Replace token.{Sink,Source} with parse.Stream{Encoder,Decoder}

## Overview

Replace `token.{Sink,Source}` with `parse.StreamEncoder` and `parse.StreamDecoder` that provide explicit stack management similar to `jsontext.{Encoder,Decoder}`. This shift focuses on streaming documents as a foundational capability, with indexing as one use case.

## Key Simplifications

1. **Bracketed-Only Streaming**: Only support bracketed structures (`{...}` and `[...]`), not block style
   - Eliminates ~100+ lines of `TArrayElt` handling
   - Makes path tracking reliable and accurate
   - Simplifies state machine significantly
   - Matches jsontext pattern (all bracketed)

2. **Eliminate token.Source**: Minimal core + convenience wrapper
   - `StreamState` - minimal core (just stack/path management, ~200 lines)
   - `StreamDecoder` - convenience wrapper (adds tokenization for io.Reader, ~400 lines)
   - Tokenization is internal (~200 lines), not a separate type
   - Can use `StreamState` directly if you have tokens

3. **Eliminate NodeParser**: `StreamDecoder.ReadValue()` replaces `NodeParser.ParseNext()`
   - Same functionality (read complete `ir.Node` values)
   - Better API (explicit stack management)
   - Eliminates ~1,282 lines of code
   - One less type to learn

4. **Sparse Arrays as Objects**: Treat sparse arrays as objects in streaming layer
   - Structural distinction (object vs array) handled at streaming layer
   - Semantic distinction (sparse array vs object) handled at parsing layer
   - Higher layers determine `!sparsearray` tag based on key types

5. **token.Tokenize Stays Non-Streaming**: Already works on byte slices
   - No changes needed to `token.Tokenize()`
   - Simple function, no streaming complexity
   - `StreamDecoder` handles streaming tokenization internally using `tokenizeOne()`

## Goals

1. **Explicit Stack Management**: Provide queryable state (depth, path, parent context) like jsontext
2. **Foundation for Streaming**: Make streaming documents a core capability, not just for indexing
3. **Clean API**: Separate concerns - tokenization vs. streaming encoding/decoding
4. **Remove Legacy Code**: Delete `token.{Sink,Source}` and `token.Source` once migration is complete
5. **Simplified Architecture**: Bracketed-only eliminates complexity, makes path tracking reliable

## Current State

### token.{Sink,Source} Status
- ✅ **Not used in codebase**: Only mentioned in docs (`docs/path_by_path_snapshotting.md`)
- ✅ **Can be removed safely**: No production dependencies
- ✅ **Path tracking exists**: Both have internal path tracking (300+ lines each)
- ⚠️ **Block style complexity**: Path tracking doesn't work for block style (`TArrayElt`)

### token.Source Status
- ✅ **Used by NodeParser**: `parse.NodeParser` uses `token.TokenSource` for incremental parsing
- ⚠️ **Path tracking embedded**: Path tracking is in token package but only works for bracketed structures
- ✅ **Can be replaced**: Replace with simpler `token.Reader` (just tokenization)

### NodeParser Status
- ✅ **Can be eliminated**: `StreamDecoder.ReadValue()` provides same functionality
- ✅ **~1,282 lines**: Implementation + tests can be removed
- ✅ **Used in**: `parse/incremental.go` + tests, `streaming_index_prototype.go`
- ✅ **Replacement**: `StreamDecoder.ReadValue()` is better (explicit stack management)

### parse Package Status
- ✅ **Core parsing logic**: `Parse()`, `ParseMulti()` work with tokenized input (unchanged)
- ✅ **token.Tokenize**: Already non-streaming (works on byte slices, no changes needed)
- ✅ **Streaming**: Will use `StreamDecoder` instead of `NodeParser`

## Design: parse.StreamEncoder and parse.StreamDecoder

### Architecture Principles

1. **Explicit State**: All state is queryable (depth, path, structure type)
2. **Stack-Based**: Explicit push/pop operations for structures
3. **Bracketed-Only**: Only support `{...}` and `[...]` structures (no block style)
4. **Token-Agnostic**: Works with `token.Token` but manages structure explicitly
5. **Offset Tracking**: Separate offset management from token stream
6. **Path Tracking**: Explicit path stack with queryable methods (in parse package, not token)
7. **Simple State Machine**: Just push/pop on brackets, no indentation tracking

### StreamEncoder API

```go
package parse

import (
    "io"
    "github.com/signadot/tony-format/go-tony/token"
)

// StreamEncoder provides explicit stack management for streaming Tony documents.
// Similar to jsontext.Encoder, but for Tony format.
type StreamEncoder struct {
    writer io.Writer
    
    // Explicit state management
    state stateMachine
    
    // Path tracking
    paths pathStack
    
    // Object key tracking
    names nameStack
    
    // Offset tracking
    offset int64
    
    // Formatting options
    opts *streamOpts
}

// stateMachine tracks structural depth and current structure type
// Simplified: Only bracketed structures, no block style
type stateMachine struct {
    depth int           // Current nesting depth
    stack []structureType // Stack of structure types (object, array)
}

type structureType int
const (
    structureNone structureType = iota
    structureObject  // { ... } - includes sparse arrays (semantic distinction at parse layer)
    structureArray   // [ ... ]
)

// pathStack tracks kinded paths explicitly
type pathStack struct {
    current string   // Current kinded path (e.g., "", "key", "key[0]")
    stack   []string // Stack of parent paths
    names   []string // Stack of object keys
    indices []int    // Stack of array indices
}

// nameStack tracks object keys for path construction
type nameStack struct {
    stack []string // Stack of object keys
}

// Queryable State Methods
func (e *StreamEncoder) Depth() int
func (e *StreamEncoder) CurrentPath() string
func (e *StreamEncoder) ParentPath() string
func (e *StreamEncoder) IsInObject() bool
func (e *StreamEncoder) IsInArray() bool
func (e *StreamEncoder) CurrentKey() string  // Current object key (if in object)
func (e *StreamEncoder) CurrentIndex() int   // Current array index (if in array)
func (e *StreamEncoder) Offset() int64

// Structure Control Methods
// Note: Sparse arrays use BeginObject/EndObject (semantic distinction at parse layer)
func (e *StreamEncoder) BeginObject() error  // { ... } - object or sparse array
func (e *StreamEncoder) EndObject() error
func (e *StreamEncoder) BeginArray() error   // [ ... ] - regular array only
func (e *StreamEncoder) EndArray() error

// Value Writing Methods
func (e *StreamEncoder) WriteKey(key string) error
func (e *StreamEncoder) WriteString(value string) error
func (e *StreamEncoder) WriteInt(value int64) error
func (e *StreamEncoder) WriteFloat(value float64) error
func (e *StreamEncoder) WriteBool(value bool) error
func (e *StreamEncoder) WriteNull() error

// Token Writing (for compatibility)
func (e *StreamEncoder) WriteToken(tok token.Token) error

// Flush and Reset
func (e *StreamEncoder) Flush() error
func (e *StreamEncoder) Reset(w io.Writer, opts ...StreamOption)
```

### StreamDecoder API

**Two-Level Design**: Minimal core + convenience wrapper

```go
package parse

// StreamState provides minimal stack/state/path management.
// Just processes tokens and tracks state - no tokenization.
type StreamState struct {
    state stateMachine
    paths pathStack
    names nameStack
    offset int64
}

func NewStreamState() *StreamState
func (s *StreamState) ProcessToken(tok token.Token) error
func (s *StreamState) Depth() int
func (s *StreamState) CurrentPath() string
func (s *StreamState) Offset() int64
// ... other query methods

// StreamDecoder provides convenience wrapper with tokenization.
// Uses StreamState internally, adds tokenization for io.Reader.
type StreamDecoder struct {
    reader io.Reader
    state *StreamState
    
    // Internal tokenization (minimal, ~200 lines, not exported)
    buf []byte
    bufPos int
    ts *token.tkState
    posDoc *token.PosDoc
}

func NewStreamDecoder(reader io.Reader) *StreamDecoder
func (d *StreamDecoder) ReadToken() (token.Token, error)  // Tokenizes internally
func (d *StreamDecoder) ProcessToken(tok token.Token) error // For pre-tokenized data
func (d *StreamDecoder) ReadValue() (*ir.Node, error)      // Builds node
// ... query methods delegate to state
```

// Queryable State Methods (same as StreamEncoder)
func (d *StreamDecoder) Depth() int
func (d *StreamDecoder) CurrentPath() string
func (d *StreamDecoder) ParentPath() string
func (d *StreamDecoder) IsInObject() bool
func (d *StreamDecoder) IsInArray() bool
func (d *StreamDecoder) CurrentKey() string
func (d *StreamDecoder) CurrentIndex() int
func (d *StreamDecoder) Offset() int64

// Structure Reading Methods
func (d *StreamDecoder) ReadToken() (token.Token, error)
func (d *StreamDecoder) ReadValue() (*ir.Node, error)  // Read complete value

// Peek Methods (without consuming)
func (d *StreamDecoder) PeekToken() (token.Token, error)

// Reset
func (d *StreamDecoder) Reset(r io.Reader, opts ...StreamOption)
```

## Implementation Plan

### Phase 0: ~~Simplify Token Package~~ → **SKIP**

**Decision**: No need for `token.Reader` - `StreamDecoder` handles tokenization internally!

**Rationale**:
- `StreamDecoder` can call `tokenizeOne()` directly
- No need for separate abstraction layer
- Simpler architecture (one less type)
- ~300-400 lines saved

### Phase 1: Core Infrastructure (Week 1)

**Goal**: Build minimal core with explicit stack management (bracketed-only)

**Tasks**:
1. ✅ Create `parse/stream_state.go` with `StreamState` (minimal core)
2. ✅ Implement `stateMachine`, `pathStack`, `nameStack`
3. ✅ Implement `ProcessToken()` - updates state/path based on token
4. ✅ Implement queryable state methods (`Depth()`, `CurrentPath()`, etc.)
5. ✅ Add tests for state management (bracketed structures only)
6. ✅ Document state machine invariants

**Files**:
- `parse/stream_state.go` - StreamState (minimal core, just stack/path management)
- `parse/stream_state_test.go` - State management tests

**Key Design Decisions**:
- **Minimal Core**: `StreamState` just processes tokens, no tokenization
- **Path Format**: Use kinded path syntax (`"key"`, `"key[0]"`, `"key{0}"`) matching existing
- **Bracketed-Only**: No `TArrayElt` handling, no block style support
- **Sparse Arrays**: Treated as objects in streaming layer (`{0: val}` uses `BeginObject()`)
- **Stack Invariants**: Document when stacks are valid (e.g., `pathStack` length == `stateMachine.depth`)
- **Thread Safety**: Not thread-safe (same as jsontext)

### Phase 2: StreamEncoder Implementation (Week 1-2)

**Goal**: Implement encoding with explicit stack management (bracketed-only)

**Tasks**:
1. ✅ Create `parse/stream_encoder.go` with basic structure
2. ✅ Implement `BeginObject/Array`, `EndObject/Array` (bracketed only)
3. ✅ Implement value writing methods (`WriteString`, `WriteInt`, etc.)
4. ✅ Implement `WriteToken()` for token-based encoding (skip `TArrayElt` if encountered)
5. ✅ Add offset tracking
6. ✅ Add formatting (indentation, spacing) - simpler without block style
7. ✅ Add tests (bracketed structures only)

**Files**:
- `parse/stream_encoder.go` - StreamEncoder implementation
- `parse/stream_encoder_test.go` - Encoder tests

**Key Design Decisions**:
- **Bracketed-Only**: Only handle `{`/`}` and `[`/`]` structures
- **Sparse Arrays**: Use `BeginObject()` - semantic distinction at parse layer
- **Formatting**: Simpler without block style - reuse logic from `token.TokenSink` but simplified
- **Offset Tracking**: Separate from token writing (like jsontext)
- **Error Handling**: Return errors immediately, don't buffer errors
- **TArrayElt**: If encountered, return error (block style not supported in streaming)

### Phase 3: StreamDecoder Implementation (Week 2)

**Goal**: Implement convenience wrapper with tokenization (bracketed-only)

**Tasks**:
1. ✅ Create `parse/stream_decoder.go` with basic structure
2. ✅ Use `StreamState` internally (delegate state management)
3. ✅ Implement minimal tokenization (~200 lines):
   - Buffer management (read from `io.Reader`, handle EOF)
   - Call `tokenizeOne()` directly
   - Handle trailing newline
4. ✅ Implement `ReadToken()` - tokenizes internally, calls `state.ProcessToken()`
5. ✅ Implement `ProcessToken()` - for pre-tokenized data (delegates to state)
6. ✅ Implement `ReadValue()` for complete value reading
7. ✅ Add `PeekToken()` for lookahead
8. ✅ Delegate query methods to `StreamState`
9. ✅ Add tests (bracketed structures only, error on `TArrayElt`)

**Files**:
- `parse/stream_decoder.go` - StreamDecoder (convenience wrapper with tokenization)
- `parse/stream_decoder_test.go` - Decoder tests

**Key Design Decisions**:
- **Minimal Core**: `StreamState` handles stack/path (no tokenization)
- **Convenience Wrapper**: `StreamDecoder` adds tokenization for `io.Reader`
- **Tokenization**: Minimal internal implementation (~200 lines, not exported)
- **Buffer Management**: StreamDecoder manages buffer (read chunks, compact when needed)
- **Bracketed-Only**: Only handle `{`/`}` and `[`/`]` structures
- **TArrayElt**: Return error if encountered (block style not supported)
- **State Updates**: `ReadToken()` calls `state.ProcessToken()` internally
- **Value Reading**: `ReadValue()` reads complete values (like `NodeParser.ParseNext()`)
- **Flexibility**: Can use `StreamState` directly if you have tokens

### Phase 4: Integration and Migration (Week 2-3)

**Goal**: Replace NodeParser usage and verify functionality

**Tasks**:
1. ✅ Update `streaming_index_prototype.go` to use `StreamDecoder.ReadValue()` instead of `NodeParser.ParseNext()`
2. ✅ Verify `StreamDecoder.ReadValue()` works (same as `NodeParser.ParseNext()`)
3. ✅ Create example usage in docs (bracketed-only)
4. ✅ Verify no code uses `token.{Sink,Source}` (already confirmed)
5. ✅ Add migration guide for future users

**Files**:
- `system/logd/storage/streaming_index_prototype.go` - Update to use `StreamDecoder`
- `docs/streaming_guide.md` - Usage documentation (bracketed-only)

**Key Design Decisions**:
- **NodeParser Replacement**: `StreamDecoder.ReadValue()` replaces `NodeParser.ParseNext()`
- **Same Functionality**: Both read complete `ir.Node` values
- **Better API**: StreamDecoder has explicit stack management
- **Backward Compatibility**: Not needed (no existing users of streaming)

### Phase 5: Remove Legacy Code (Week 3)

**Goal**: Remove `token.{Sink,Source}`, `token.Source`, and `NodeParser`

**Tasks**:
1. ✅ Remove `token/sink.go` (~542 lines)
2. ✅ Remove `token/source.go` (~603 lines, replaced by `token.Reader`)
3. ✅ Remove `token/sink_test.go` (if exists)
4. ✅ Remove `token/source_test.go` (if exists)
5. ✅ Remove `parse/incremental.go` (~422 lines)
6. ✅ Remove `parse/incremental_test.go` (~310 lines)
7. ✅ Remove `parse/incremental_positions_test.go` (~270 lines)
8. ✅ Remove `parse/incremental_simple_value_test.go` (~280 lines)
9. ✅ Update `token/doc.go` to remove references
10. ✅ Search codebase for any remaining references
11. ✅ Update docs that mention `token.{Sink,Source}`, `token.Source`, or `NodeParser`

**Files to Delete**:
- `token/sink.go` (~542 lines)
- `token/source.go` (~603 lines, replaced by `token/reader.go`)
- `token/sink_test.go` (if exists)
- `token/source_test.go` (if exists)
- `parse/incremental.go` (~422 lines)
- `parse/incremental_test.go` (~310 lines)
- `parse/incremental_positions_test.go` (~270 lines)
- `parse/incremental_simple_value_test.go` (~280 lines)

**Total Elimination**: ~2,427 lines of code!

**Files to Update**:
- `token/doc.go` - Remove Sink/Source documentation, document `token.Reader`
- `parse/doc.go` - Remove NodeParser documentation, document `StreamDecoder`
- `docs/path_by_path_snapshotting.md` - Update references
- `system/logd/storage/streaming_index_prototype.go` - Already updated to use `StreamDecoder`
- Any other docs mentioning `token.{Sink,Source}`, `token.Source`, or `NodeParser`

## Detailed API Design

### StreamEncoder Example Usage

```go
// Basic encoding
enc := parse.NewStreamEncoder(writer)
enc.BeginObject()
enc.WriteKey("name")
enc.WriteString("value")
enc.WriteKey("array")
enc.BeginArray()
enc.WriteInt(1)
enc.WriteInt(2)
enc.EndArray()
enc.EndObject()
enc.Flush()

// With path tracking for indexing
enc := parse.NewStreamEncoder(writer)
enc.BeginObject()
path := enc.CurrentPath()  // ""
enc.WriteKey("key")
path = enc.CurrentPath()    // "key"
offset := enc.Offset()       // Byte offset at "key"
// Build index entry
enc.WriteString("value")
enc.EndObject()

// Token-based encoding (for compatibility)
enc := parse.NewStreamEncoder(writer)
tokens := []token.Token{
    token.BeginObject(),
    token.String("key"),
    token.Colon(),
    token.String("value"),
    token.EndObject(),
}
for _, tok := range tokens {
    enc.WriteToken(tok)
}
```

### StreamDecoder Example Usage

```go
// Basic decoding (replaces NodeParser.ParseNext())
dec := parse.NewStreamDecoder(reader)
for {
    node, err := dec.ReadValue()  // Same as NodeParser.ParseNext()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    // Process complete value
    processNode(node)
}

// Token-based decoding (for fine-grained control)
dec := parse.NewStreamDecoder(reader)
for {
    tok, err := dec.ReadToken()  // Tokenizes internally
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    // Process token
    processToken(tok)
}

// With path tracking for indexing
dec := parse.NewStreamDecoder(reader)
for {
    tok, err := dec.ReadToken()
    if err == io.EOF {
        break
    }
    path := dec.CurrentPath()  // Query current path
    depth := dec.Depth()        // Query current depth
    offset := dec.Offset()      // Query current offset
    
    // Build index at boundaries
    if dec.IsInObject() && tok.Type == token.TColon {
        // Object key boundary
        key := dec.CurrentKey()
        buildIndexEntry(key, path, offset)
    }
}

// Using StreamState directly (if you have tokens)
state := parse.NewStreamState()
tokens := token.Tokenize(nil, data)  // Pre-tokenized
for _, tok := range tokens {
    state.ProcessToken(tok)
    path := state.CurrentPath()  // Query state
    // Build index, etc.
}
```

## Testing Strategy

### Unit Tests

1. **State Management Tests** (`stream_state_test.go`):
   - Stack push/pop operations
   - Path tracking correctness
   - Depth tracking
   - Invariant checks

2. **Encoder Tests** (`stream_encoder_test.go`):
   - Basic encoding (objects, arrays, values)
   - Path tracking during encoding
   - Offset tracking
   - Formatting (indentation, spacing)
   - Error handling

3. **Decoder Tests** (`stream_decoder_test.go`):
   - Basic decoding (objects, arrays, values)
   - Path tracking during decoding
   - Offset tracking
   - Peek operations
   - Error handling

### Integration Tests

1. **Round-Trip Tests**:
   - Encode → Decode → Verify
   - Path tracking consistency
   - Offset accuracy

2. **Indexing Tests**:
   - Build index using StreamEncoder
   - Query index using StreamDecoder
   - Verify index accuracy

### Compatibility Tests

1. **Parse Compatibility**:
   - StreamEncoder output matches `Parse()` output
   - StreamDecoder output matches `Parse()` output

## Migration Checklist

### Pre-Migration
- [ ] Confirm no code uses `token.{Sink,Source}` ✅ (already confirmed)
- [ ] Review `token` package for dependencies
- [ ] Document current `token.{Sink,Source}` behavior (for reference)

### Implementation
- [ ] Phase 1: Core infrastructure
- [ ] Phase 2: StreamEncoder
- [ ] Phase 3: StreamDecoder
- [ ] Phase 4: Integration
- [ ] Phase 5: Remove legacy code

### Post-Migration
- [ ] Update all documentation
- [ ] Verify tests pass
- [ ] Update examples
- [ ] Create migration guide (for future reference)

## Risk Assessment

### Low Risk
- ✅ **No existing users**: `token.{Sink,Source}` not used in codebase
- ✅ **Clear API design**: Based on proven jsontext pattern
- ✅ **Incremental implementation**: Can test each phase independently
- ✅ **Bracketed-only simplifies**: Eliminates ~100+ lines of complexity
- ✅ **NodeParser replacement**: `StreamDecoder.ReadValue()` provides same functionality
- ✅ **token.Tokenize unchanged**: Already non-streaming, no changes needed

### Medium Risk
- ⚠️ **Path tracking migration**: Moving from token package to parse package (mitigated by tests)
- ⚠️ **Formatting compatibility**: Need to match existing formatting (mitigated by tests)
- ⚠️ **NodeParser elimination**: Need to verify `StreamDecoder.ReadValue()` matches `NodeParser.ParseNext()` behavior

### Mitigation Strategies
1. **Comprehensive tests**: Test all state transitions (bracketed structures)
2. **Incremental rollout**: Implement and test each phase
3. **Documentation**: Clear API docs with examples (bracketed-only)
4. **Simplified architecture**: Bracketed-only reduces complexity significantly
5. **token.Reader first**: Create `token.Reader` first, then StreamDecoder, then remove legacy code
6. **NodeParser parity**: Ensure `StreamDecoder.ReadValue()` matches `NodeParser.ParseNext()` behavior

## Success Criteria

1. ✅ **Explicit Stack Management**: All state queryable via methods
2. ✅ **Path Tracking**: Accurate path tracking during encode/decode
3. ✅ **Offset Tracking**: Accurate byte offset tracking
4. ✅ **Formatting**: Output matches existing formatting
5. ✅ **Tests**: Comprehensive test coverage (>90%)
6. ✅ **Documentation**: Clear API docs and examples
7. ✅ **Legacy Removal**: `token.{Sink,Source}`, `token.Source`, and `NodeParser` removed
8. ✅ **Code Reduction**: Net reduction of ~600-1,000 lines (25-40%)
9. ✅ **Functionality Parity**: `StreamDecoder.ReadValue()` matches `NodeParser.ParseNext()` behavior

## Future Enhancements

After migration, consider:

1. **Performance Optimizations**:
   - Buffer pooling
   - Zero-allocation paths
   - SIMD optimizations

2. **Additional Features**:
   - Streaming validation
   - Streaming transformation
   - Streaming merge operations

3. **Indexing Integration**:
   - Built-in index building helpers
   - Index query helpers
   - Index optimization utilities

## Timeline Estimate

- **Week 1**: Phase 1 (Core Infrastructure) + Phase 2 start (StreamEncoder)
- **Week 2**: Phase 2 complete (StreamEncoder) + Phase 3 (StreamDecoder - includes tokenization)
- **Week 3**: Phase 4 (Integration) + Phase 5 (Remove legacy code)

**Total**: ~3 weeks for complete migration

**Simplifications reduce timeline**:
- Bracketed-only eliminates ~100+ lines of complexity
- No token.Reader needed (saves ~300-400 lines)
- Simpler state machine = faster implementation
- Less edge cases to handle

## Next Steps

1. **Review this plan** with team
2. **Start Phase 0**: Create `token.Reader` (simplified tokenization)
3. **Start Phase 1**: Implement core infrastructure (bracketed-only)
4. **Iterate**: Build, test, refine
5. **Document**: Update docs as we go (bracketed-only examples)

## Key Simplifications Summary

### Code Elimination Summary

**Files Eliminated** (~2,427 lines):
- `parse/incremental.go` + tests (~1,282 lines) - NodeParser
- `token/sink.go` + tests (~542 lines) - TokenSink
- `token/source.go` + tests (~603 lines) - TokenSource

**Files Added** (~1,000-1,300 lines):
- `parse/stream_encoder.go` + tests (~400-500 lines)
- `parse/stream_state.go` + tests (~200-300 lines) - minimal core (just stack/path)
- `parse/stream_decoder.go` + tests (~400-500 lines) - convenience wrapper (tokenization ~200 lines)
- ~~`token/reader.go`~~ - **NOT NEEDED!** StreamDecoder handles tokenization internally

**Net Reduction**: ~1,100-1,400 lines (45-55% reduction!)

**Architecture**:
- **StreamState**: Minimal core (~200 lines) - just stack/path management
- **StreamDecoder**: Convenience wrapper (~400 lines) - adds tokenization for io.Reader
- **Total**: ~600 lines (vs ~2,427 eliminated)

### Bracketed-Only Benefits
- ✅ **~100+ lines eliminated**: No `TArrayElt` handling
- ✅ **Reliable path tracking**: Explicit boundaries make tracking accurate
- ✅ **Simple state machine**: Just push/pop on brackets
- ✅ **Matches jsontext**: All bracketed, proven pattern

### Eliminate token.Source Benefits
- ✅ **Minimal core**: `StreamState` is just stack/path management (~200 lines)
- ✅ **Convenience wrapper**: `StreamDecoder` adds tokenization (~400 lines total)
- ✅ **No separate tokenizer**: Tokenization is internal, not exported
- ✅ **Flexible**: Can use `StreamState` directly if you have tokens
- ✅ **Less code**: ~1,100-1,400 lines saved (45-55% reduction)

### Eliminate NodeParser Benefits
- ✅ **~1,282 lines eliminated**: NodeParser implementation + tests
- ✅ **Better API**: `StreamDecoder.ReadValue()` with explicit stack management
- ✅ **Same functionality**: Reads complete `ir.Node` values
- ✅ **One less type**: Simpler API surface

### Sparse Arrays as Objects Benefits
- ✅ **Simpler API**: Just `BeginObject()` vs. separate sparse array methods
- ✅ **Semantic distinction**: Handled at parse layer where it belongs
- ✅ **Matches reality**: Structurally objects, semantically arrays

### token.Tokenize Stays Simple
- ✅ **No changes needed**: Already non-streaming (works on byte slices)
- ✅ **Simple function**: No streaming complexity
- ✅ **StreamDecoder**: Handles streaming tokenization internally using `tokenizeOne()`
