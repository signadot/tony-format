# Analysis: Does StreamDecoder Need tokenizeOne?

## Question

Why does `StreamDecoder` need `tokenizeOne()`? Can it work with explicit tokens instead?

## Current Plan

```go
type StreamDecoder struct {
    reader io.Reader
    buf []byte
    // ... calls tokenizeOne() internally
}

func (d *StreamDecoder) ReadToken() (token.Token, error) {
    // Internally calls tokenizeOne() to tokenize from buffer
    tokens, err := tokenizeOne(...)
    // Updates state, path tracking
}
```

## Alternative: Explicit Token API

### Option 1: StreamDecoder Works With Tokens Only

```go
type StreamDecoder struct {
    // No io.Reader, no buffering, no tokenization
    state stateMachine
    paths pathStack
    // ...
}

func NewStreamDecoder() *StreamDecoder

func (d *StreamDecoder) ProcessToken(tok token.Token) error {
    // Update state, path tracking based on token
    d.state.update(tok)
    d.paths.update(tok)
    return nil
}

func (d *StreamDecoder) ReadValue() (*ir.Node, error) {
    // Build ir.Node from processed tokens
    // But... where do tokens come from?
}
```

**Problem**: How do we get tokens from `io.Reader`? Still need tokenization somewhere.

### Option 2: Separate Tokenization, StreamDecoder Manages Stack Only

```go
// Tokenization is separate
tokens, err := tokenizeFromReader(reader)  // Or use token.Tokenize on chunks

// StreamDecoder just manages stack/path
dec := parse.NewStreamDecoder()
for _, tok := range tokens {
    dec.ProcessToken(tok)  // Updates state, path
    if dec.IsValueBoundary() {
        node := dec.BuildNode()  // Build ir.Node from processed tokens
    }
}
```

**Pros**:
- ✅ Clear separation: tokenization vs. stack management
- ✅ StreamDecoder is simpler (no buffering, no tokenization)
- ✅ Can work with pre-tokenized data

**Cons**:
- ⚠️ Need to tokenize entire document first (or chunk it)
- ⚠️ Less convenient for streaming use cases

### Option 3: Two-Level API

```go
// Low-level: Explicit token processing
type StreamDecoder struct {
    state stateMachine
    paths pathStack
    // No tokenization
}

func (d *StreamDecoder) ProcessToken(tok token.Token) error
func (d *StreamDecoder) ReadValue() (*ir.Node, error)  // Builds from processed tokens

// High-level: Convenience wrapper
type StreamingDecoder struct {
    reader io.Reader
    decoder *StreamDecoder
    tokenizer *token.StreamTokenizer  // Simple tokenizer
}

func (sd *StreamingDecoder) ReadValue() (*ir.Node, error) {
    // Read tokens, process them, build node
    for {
        tok, err := sd.tokenizer.ReadToken()
        if err != nil {
            return nil, err
        }
        sd.decoder.ProcessToken(tok)
        if sd.decoder.IsValueComplete() {
            return sd.decoder.ReadValue()
        }
    }
}
```

**Pros**:
- ✅ Clear separation of concerns
- ✅ Low-level API is simple (just stack management)
- ✅ High-level API provides convenience

**Cons**:
- ⚠️ Two types to learn
- ⚠️ Still need tokenization somewhere

## What tokenizeOne Actually Does

Looking at `tokenize_one.go`:
- Parses tokens from buffer (handles all token types)
- Returns `io.EOF` if needs more buffer (for streaming)
- Maintains tokenization state (`tkState`)
- Handles multiline strings, comments, etc.

**Key Insight**: `tokenizeOne()` is needed for streaming tokenization from `io.Reader`.

## The Real Question

**Do we need streaming tokenization, or can we tokenize in chunks?**

### Current token.Tokenize()
```go
func Tokenize(dst []Token, src []byte, ...) ([]Token, error)
```

Works on byte slices. For streaming:
- Read chunk from `io.Reader` → `[]byte`
- Call `Tokenize()` on chunk
- But... tokens might span chunks (multiline strings, etc.)

**Problem**: `Tokenize()` expects complete document (or at least complete tokens).

### tokenizeOne() for Streaming
- Works on partial buffers
- Returns `io.EOF` if needs more data
- Handles tokens that span buffers

**This is why token.Source exists** - it manages buffering and calls `tokenizeOne()`.

## Proposed: Minimal Tokenization Layer

Instead of `token.Reader` or `tokenizeOne()` in StreamDecoder, what if we have:

```go
// Minimal streaming tokenizer (just buffering + tokenizeOne)
type tokenStreamer struct {
    reader io.Reader
    buf []byte
    ts *token.tkState
    // ... minimal buffering logic
}

func (ts *tokenStreamer) ReadToken() (token.Token, error) {
    // Ensure buffer has data
    // Call tokenizeOne()
    // Return token
}

// StreamDecoder uses tokenStreamer internally
type StreamDecoder struct {
    streamer *tokenStreamer  // Internal, not exported
    state stateMachine
    paths pathStack
}

func NewStreamDecoder(reader io.Reader) *StreamDecoder {
    return &StreamDecoder{
        streamer: newTokenStreamer(reader),
        // ...
    }
}

func (d *StreamDecoder) ReadToken() (token.Token, error) {
    tok, err := d.streamer.ReadToken()
    if err != nil {
        return nil, err
    }
    // Update state, path tracking
    d.state.update(tok)
    d.paths.update(tok)
    return tok, nil
}
```

**This is essentially what we had with token.Reader!**

## Alternative: Require Explicit Stack Operations

What if StreamDecoder doesn't tokenize at all, and requires explicit structure operations?

```go
type StreamDecoder struct {
    state stateMachine
    paths pathStack
    // No tokenization, no io.Reader
}

func (d *StreamDecoder) BeginObject() error
func (d *StreamDecoder) EndObject() error
func (d *StreamDecoder) BeginArray() error
func (d *StreamDecoder) EndArray() error
func (d *StreamDecoder) ProcessValue(value interface{}) error
func (d *StreamDecoder) ReadValue() (*ir.Node, error)
```

**But then**: How do we read from `io.Reader`? Still need tokenization somewhere.

## The Core Issue

**Tokenization is necessary** for reading from `io.Reader`. The question is:

1. **Where does tokenization live?**
   - Option A: In StreamDecoder (current plan)
   - Option B: Separate tokenizer, StreamDecoder uses it
   - Option C: Caller provides tokens, StreamDecoder just manages stack

2. **What level of abstraction?**
   - Low-level: Explicit tokens → Stack management
   - High-level: io.Reader → Complete values

## Recommendation: Keep Tokenization in StreamDecoder

**Why**:
- ✅ **Convenience**: Most use cases read from `io.Reader`
- ✅ **Efficiency**: Can stream without loading entire document
- ✅ **Simplicity**: One type, one API
- ✅ **Necessary**: Need tokenization anyway for `io.Reader`

**But make it minimal**:
- Internal `tokenStreamer` (not exported)
- Just buffering + `tokenizeOne()` calls
- ~200 lines (vs ~600 for token.Source)

**Alternative if we want explicit tokens**:
- Provide `StreamDecoder.ProcessToken()` for pre-tokenized data
- `ReadToken()` uses internal tokenization
- Best of both worlds

## Final Architecture

```go
type StreamDecoder struct {
    // Internal tokenization (minimal)
    streamer *tokenStreamer  // ~200 lines, not exported
    
    // Explicit stack management (public API)
    state stateMachine
    paths pathStack
    
    // Public methods
    ReadToken() (token.Token, error)      // Uses streamer internally
    ProcessToken(tok token.Token) error   // For pre-tokenized data
    ReadValue() (*ir.Node, error)         // Builds from processed tokens
}
```

**Best of both worlds**:
- Convenient: `ReadToken()` handles tokenization
- Flexible: `ProcessToken()` for explicit control
- Simple: Tokenization is internal, not a separate type
