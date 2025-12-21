# Analysis: Eliminating NodeParser

## Insight

Since `parse.StreamDecoder` provides `ReadValue()` which reads complete `ir.Node` values, `NodeParser` becomes redundant. We can eliminate it entirely!

## Current Code Complexity

### Lines of Code Analysis

**Files to Eliminate**:
- `parse/incremental.go` - ~422 lines (NodeParser implementation)
- `parse/incremental_test.go` - ~310 lines (NodeParser tests)
- `parse/incremental_positions_test.go` - ~270 lines (position tracking tests)
- `parse/incremental_simple_value_test.go` - ~280 lines (simple value tests)
- `token/sink.go` - ~542 lines (TokenSink)
- `token/source.go` - ~603 lines (TokenSource)

**Total**: ~2,427 lines of code to eliminate!

### Function/Type Count

- **NodeParser**: 5 exported functions/types
- **TokenSink**: 10 exported functions/types  
- **TokenSource**: 15 exported functions/types

**Total**: ~30 exported APIs to eliminate

## Replacement Strategy

### StreamDecoder.ReadValue() Replaces NodeParser.ParseNext()

**Current NodeParser**:
```go
parser := parse.NewNodeParser(tokenSource)
node, err := parser.ParseNext()  // Reads complete ir.Node
```

**New StreamDecoder**:
```go
dec := parse.NewStreamDecoder(reader)
node, err := dec.ReadValue()  // Reads complete ir.Node
```

**Benefits**:
- ✅ Same functionality (read complete values)
- ✅ Explicit stack management (better for indexing)
- ✅ Queryable state (depth, path, etc.)
- ✅ Simpler API (one less type to learn)

### token.Tokenize Can Be Non-Streaming

**Current**: `token.Tokenize(dst []Token, src []byte, ...)` - already non-streaming!

**Analysis**:
- `token.Tokenize` already works on byte slices (not `io.Reader`)
- No streaming complexity needed
- Can stay as-is (simple function)

**What Changes**:
- Remove streaming tokenization from `token` package
- Keep `token.Tokenize()` for byte slice tokenization
- `token.Reader` (new) for streaming tokenization (used by StreamDecoder)

## Code Elimination Summary

### What We Remove

1. **NodeParser** (~1,282 lines):
   - `parse/incremental.go` - implementation
   - `parse/incremental_test.go` - tests
   - `parse/incremental_positions_test.go` - position tests
   - `parse/incremental_simple_value_test.go` - simple value tests

2. **TokenSink** (~542 lines):
   - `token/sink.go` - implementation
   - `token/sink_test.go` - tests (if exists)

3. **TokenSource** (~603 lines):
   - `token/source.go` - implementation  
   - `token/source_test.go` - tests (if exists)

**Total Elimination**: ~2,427 lines

### What We Add

1. **StreamEncoder** (~400-500 lines estimated):
   - `parse/stream_encoder.go` - implementation
   - `parse/stream_encoder_test.go` - tests

2. **StreamDecoder** (~500-600 lines estimated):
   - `parse/stream_decoder.go` - implementation
   - `parse/stream_decoder_test.go` - tests
   - Includes `ReadValue()` (replaces NodeParser)

3. **Stream State** (~200-300 lines estimated):
   - `parse/stream_state.go` - state management
   - `parse/stream_state_test.go` - tests

4. **token.Reader** (~300-400 lines estimated):
   - `token/reader.go` - simple tokenization
   - `token/reader_test.go` - tests

**Total Addition**: ~1,400-1,800 lines

### Net Reduction

**Eliminated**: ~2,427 lines
**Added**: ~1,400-1,800 lines
**Net Reduction**: ~600-1,000 lines (25-40% reduction!)

## Complexity Reduction

### Before (Current State)

```
token.Tokenize() → []Token (non-streaming)
token.TokenSource → []Token (streaming, with path tracking)
parse.NodeParser → *ir.Node (uses TokenSource)
token.TokenSink → io.Writer (with path tracking)
```

**Complexity**:
- 3 different tokenization paths
- Path tracking in 2 places (Source, Sink)
- NodeParser has its own buffering logic
- ~2,427 lines of code

### After (Simplified State)

```
token.Tokenize() → []Token (non-streaming, simple)
token.Reader → []Token (streaming, simple - no path tracking)
parse.StreamDecoder → *ir.Node (uses token.Reader, has path tracking)
parse.StreamEncoder → io.Writer (has path tracking)
```

**Complexity**:
- 2 tokenization paths (non-streaming + streaming)
- Path tracking in 1 place (parse package)
- StreamDecoder handles buffering + parsing
- ~1,400-1,800 lines of code

## Migration Impact

### API Changes

**Old**:
```go
// Incremental parsing
source := token.NewTokenSource(reader)
parser := parse.NewNodeParser(source)
node, err := parser.ParseNext()

// Encoding
sink := token.NewTokenSink(writer, callback)
sink.Write(tokens)
```

**New**:
```go
// Incremental parsing (replaces NodeParser)
dec := parse.NewStreamDecoder(reader)
node, err := dec.ReadValue()  // Same as ParseNext()

// Encoding (replaces TokenSink)
enc := parse.NewStreamEncoder(writer)
enc.BeginObject()
enc.WriteKey("key")
enc.WriteString("value")
enc.EndObject()
```

### Benefits

1. **Simpler API**: One less type (`NodeParser`)
2. **Better for Indexing**: Explicit stack management
3. **Queryable State**: Can query depth, path, etc.
4. **Less Code**: ~600-1,000 lines reduction
5. **Clearer Separation**: Tokenization vs. streaming vs. parsing

## Updated Migration Plan

### Phase 0: Create token.Reader
- Create simple token reader (no path tracking)
- ~300-400 lines

### Phase 1: Core Infrastructure
- State management for StreamEncoder/Decoder
- ~200-300 lines

### Phase 2: StreamEncoder
- Replaces TokenSink
- ~400-500 lines

### Phase 3: StreamDecoder  
- Replaces NodeParser + TokenSource
- Includes `ReadValue()` method
- ~500-600 lines

### Phase 4: Integration
- Update any code using NodeParser to use StreamDecoder
- Verify functionality

### Phase 5: Remove Legacy Code
- Delete `parse/incremental.go` and tests
- Delete `token/sink.go` and tests
- Delete `token/source.go` and tests
- **Total**: ~2,427 lines eliminated!

## Conclusion

**YES, we can eliminate NodeParser!**

- `StreamDecoder.ReadValue()` provides same functionality
- Eliminates ~2,427 lines of code
- Net reduction of ~600-1,000 lines (25-40%)
- Simpler architecture
- Better for indexing use cases

**Key Insight**: `StreamDecoder.ReadValue()` is a better `NodeParser.ParseNext()` with explicit stack management!
