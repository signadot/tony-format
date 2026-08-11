# Analysis: Do We Need token.Reader?

## Question

Why do we need `token.Reader` at all? Can `StreamDecoder` handle tokenization internally?

## Current Tokenization Architecture

### token.Tokenize() (Non-Streaming)
- Works on byte slices: `Tokenize(dst []Token, src []byte, ...)`
- Calls `tokenizeOne()` internally
- Simple function, no streaming complexity

### token.Source (Streaming - to be eliminated)
- Works on `io.Reader`
- Manages buffer (reading chunks)
- Calls `tokenizeOne()` with buffer
- Handles EOF, trailing newline, buffer compaction
- **~603 lines** of buffering/streaming logic

### tokenizeOne() (Core Function)
- Works on buffer slices (can be partial)
- Handles streaming tokenization
- Returns `io.EOF` if needs more buffer
- **Already supports streaming!**

## What StreamDecoder Needs

`StreamDecoder` needs to:
1. Read tokens from `io.Reader`
2. Manage buffering (read chunks, handle EOF)
3. Call tokenization logic
4. Track state (for path tracking, etc.)

## Option 1: Use token.Reader (Current Plan)

```go
type StreamDecoder struct {
    reader io.Reader
    tokenReader *token.Reader  // Separate token reader
    // ... state management
}

func (d *StreamDecoder) ReadToken() (token.Token, error) {
    tok, err := d.tokenReader.Read()
    // Update state, path tracking, etc.
}
```

**Pros**:
- Separation of concerns (tokenization vs. streaming)
- Reusable token reader

**Cons**:
- Extra abstraction layer
- More code (~300-400 lines for token.Reader)
- StreamDecoder still needs to manage state anyway

## Option 2: StreamDecoder Handles Tokenization Internally (Better!)

```go
type StreamDecoder struct {
    reader io.Reader
    buf []byte           // Internal buffer
    bufPos int           // Position in buffer
    ts *token.tkState    // Tokenization state
    posDoc *token.PosDoc // Position tracking
    // ... state management (path tracking, etc.)
}

func (d *StreamDecoder) ReadToken() (token.Token, error) {
    // Ensure buffer has data
    if err := d.ensureBuffer(); err != nil {
        return nil, err
    }
    
    // Call tokenizeOne() directly
    tokens, consumed, err := tokenizeOne(
        d.buf, d.bufPos, d.bufStart,
        d.ts, d.posDoc, d.opts,
        d.lastToken, d.recentTokens, d.recentBuf,
        nil, nil, // allTokens, docPrefix (nil for streaming)
    )
    
    // Update buffer position
    d.bufPos += consumed
    
    // Update state, path tracking, etc.
    // ...
    
    return tokens[0], nil
}
```

**Pros**:
- ✅ **No extra abstraction** - StreamDecoder does it all
- ✅ **Less code** - Eliminates ~300-400 lines for token.Reader
- ✅ **Simpler architecture** - One less type
- ✅ **Direct control** - StreamDecoder manages everything

**Cons**:
- StreamDecoder needs to handle buffering (but it's simple)

## What token.Source Actually Does

Looking at `token/source.go`, it does:
1. **Buffer management** (~100 lines):
   - Read chunks from `io.Reader`
   - Buffer compaction (move remaining data to front)
   - Track buffer position

2. **EOF handling** (~50 lines):
   - Detect EOF
   - Add trailing newline if needed
   - Handle `tokenizeOne()` returning `io.EOF`

3. **Tokenization** (~50 lines):
   - Call `tokenizeOne()` with buffer
   - Handle initial indent
   - Update sliding windows

4. **Path tracking** (~300 lines):
   - **This moves to StreamDecoder anyway!**

**Total**: ~500 lines, but ~300 lines are path tracking (which StreamDecoder needs to do)

## StreamDecoder Can Do It All

`StreamDecoder` needs:
- ✅ Buffer management (simple - read chunks, compact when needed)
- ✅ EOF handling (simple - detect EOF, add trailing newline)
- ✅ Call `tokenizeOne()` (direct call, no wrapper needed)
- ✅ State management (already doing this for path tracking)

**Conclusion**: StreamDecoder can handle tokenization internally!

## Updated Architecture

### Before (With token.Reader)
```
io.Reader → token.Reader → []Token → StreamDecoder → *ir.Node
           (~300-400 lines)
```

### After (No token.Reader)
```
io.Reader → StreamDecoder (tokenization + parsing) → *ir.Node
           (tokenization ~200 lines, parsing ~500 lines)
```

## Code Reduction

**Eliminated**:
- `token.Reader` (~300-400 lines) - **Not needed!**

**StreamDecoder gains**:
- Tokenization logic (~200 lines) - simpler than token.Source because:
  - No path tracking (moved to StreamDecoder state)
  - No complex sliding windows (can simplify)
  - Direct integration with state management

**Net**: ~100-200 lines saved by eliminating token.Reader abstraction

## Recommendation

**NO, we don't need `token.Reader`!**

`StreamDecoder` should handle tokenization internally by:
1. Managing its own buffer (read from `io.Reader`)
2. Calling `tokenizeOne()` directly
3. Handling EOF and trailing newline
4. Managing tokenization state (`tkState`, `PosDoc`)

This is simpler, has less code, and gives `StreamDecoder` full control.

## Updated Migration Plan

### Phase 0: ~~Create token.Reader~~ → **SKIP**

### Phase 1: Core Infrastructure
- State management for StreamEncoder/Decoder
- **StreamDecoder includes tokenization state**

### Phase 2: StreamEncoder
- Replaces TokenSink
- ~400-500 lines

### Phase 3: StreamDecoder
- Replaces NodeParser + TokenSource
- **Includes tokenization internally** (no token.Reader)
- Calls `tokenizeOne()` directly
- ~600-700 lines (includes tokenization)

### Phase 4: Integration
- Update `streaming_index_prototype.go` to use StreamDecoder
- Verify functionality

### Phase 5: Remove Legacy Code
- Delete `parse/incremental.go` + tests (~1,282 lines)
- Delete `token/sink.go` + tests (~542 lines)
- Delete `token/source.go` + tests (~603 lines)
- **Total**: ~2,427 lines eliminated!

## Final Architecture

```
token.Tokenize() → []Token (non-streaming, byte slices)
                  ↓
parse.StreamDecoder → *ir.Node (streaming, uses tokenizeOne() internally)
parse.StreamEncoder → io.Writer (streaming, explicit stack management)
```

**Clean and simple!**
