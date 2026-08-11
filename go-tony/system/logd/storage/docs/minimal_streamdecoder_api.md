# Minimal StreamDecoder API: No Tokenization

## Insight

What if `StreamDecoder` doesn't tokenize at all? Just manages stack/state/path tracking, caller provides tokens explicitly?

## Option 1: Token-Only API (Minimal)

```go
type StreamDecoder struct {
    state stateMachine
    paths pathStack
    // No io.Reader, no buffering, no tokenization
}

func NewStreamDecoder() *StreamDecoder

// Process a token, update state/path
func (d *StreamDecoder) ProcessToken(tok token.Token) error

// Query state
func (d *StreamDecoder) Depth() int
func (d *StreamDecoder) CurrentPath() string
func (d *StreamDecoder) Offset() int64  // Tracks offset from tokens

// Build node from processed tokens (if we're tracking them)
func (d *StreamDecoder) ReadValue() (*ir.Node, error)  // But... needs tokens
```

**Problem**: How do we build `ir.Node`? Need to track tokens or have them available.

## Option 2: Structure-Only API (Most Minimal)

```go
type StreamDecoder struct {
    state stateMachine
    paths pathStack
    // No tokens, no tokenization
}

func NewStreamDecoder() *StreamDecoder

// Explicit structure operations
func (d *StreamDecoder) BeginObject() error
func (d *StreamDecoder) EndObject() error
func (d *StreamDecoder) BeginArray() error
func (d *StreamDecoder) EndArray() error
func (d *StreamDecoder) ProcessKey(key string) error
func (d *StreamDecoder) ProcessValue(value interface{}) error

// Query state
func (d *StreamDecoder) Depth() int
func (d *StreamDecoder) CurrentPath() string
```

**But**: This doesn't read from `io.Reader` - caller must parse first!

## Option 3: Two APIs (Best of Both Worlds)

### Low-Level: Stack Management Only

```go
// Just stack/state/path management
type StreamState struct {
    state stateMachine
    paths pathStack
}

func NewStreamState() *StreamState
func (s *StreamState) ProcessToken(tok token.Token) error
func (s *StreamState) Depth() int
func (s *StreamState) CurrentPath() string
```

### High-Level: Convenience Wrapper

```go
// Convenience wrapper that handles tokenization
type StreamDecoder struct {
    reader io.Reader
    state *StreamState
    // Internal tokenization (minimal)
}

func NewStreamDecoder(reader io.Reader) *StreamDecoder
func (d *StreamDecoder) ReadToken() (token.Token, error)  // Tokenizes internally
func (d *StreamDecoder) ReadValue() (*ir.Node, error)     // Builds node
```

**But**: Still need tokenization somewhere for `io.Reader` case.

## The Real Question: What's the Use Case?

### Use Case 1: Read from io.Reader
- **Need**: Tokenization from `io.Reader`
- **Options**:
  - StreamDecoder handles it (convenient)
  - Separate tokenizer, StreamDecoder uses it
  - Caller tokenizes first, StreamDecoder processes tokens

### Use Case 2: Process Pre-Tokenized Data
- **Need**: Just stack/path tracking
- **Options**:
  - StreamDecoder.ProcessToken() (no tokenization needed)
  - Separate StreamState type (minimal)

### Use Case 3: Build Index While Reading
- **Need**: Track paths/offsets while reading
- **Options**:
  - StreamDecoder with ReadToken() (tracks internally)
  - StreamState with ProcessToken() (caller provides tokens)

## Proposed: Minimal API with Optional Tokenization

```go
// Core: Just stack/state/path management
type StreamState struct {
    state stateMachine
    paths pathStack
    offset int64
}

func NewStreamState() *StreamState
func (s *StreamState) ProcessToken(tok token.Token) error
func (s *StreamState) Depth() int
func (s *StreamState) CurrentPath() string
func (s *StreamState) Offset() int64

// Convenience: Handles tokenization for io.Reader
type StreamDecoder struct {
    reader io.Reader
    state *StreamState
    // Minimal internal tokenization (~200 lines)
    buf []byte
    ts *token.tkState
}

func NewStreamDecoder(reader io.Reader) *StreamDecoder
func (d *StreamDecoder) ReadToken() (token.Token, error) {
    // Tokenize internally, call state.ProcessToken()
    tok := d.tokenizeNext()
    d.state.ProcessToken(tok)
    return tok, nil
}
func (d *StreamDecoder) ReadValue() (*ir.Node, error) {
    // Read tokens, build node
}
```

**Benefits**:
- ✅ Minimal core (`StreamState` - just stack management)
- ✅ Convenience wrapper (`StreamDecoder` - handles tokenization)
- ✅ Can use `StreamState` directly if you have tokens
- ✅ Tokenization is internal, not a separate type

## Alternative: Require Explicit Structure Operations

What if we go even simpler - no tokens at all?

```go
type StreamDecoder struct {
    state stateMachine
    paths pathStack
}

func (d *StreamDecoder) BeginObject() error
func (d *StreamDecoder) EndObject() error
func (d *StreamDecoder) BeginArray() error
func (d *StreamDecoder) EndArray() error
func (d *StreamDecoder) ProcessKey(key string) error
func (d *StreamDecoder) ProcessValue(value interface{}) error
func (d *StreamDecoder) Depth() int
func (d *StreamDecoder) CurrentPath() string
```

**But then**: How do we read from `io.Reader`? Need parsing/tokenization somewhere else.

**Maybe**: This is for building indexes while encoding, not decoding?

## Recommendation: Minimal Core + Optional Tokenization

**Core API** (minimal, no tokenization):
```go
type StreamState struct {
    // Just stack/state/path
}

func (s *StreamState) ProcessToken(tok token.Token) error
func (s *StreamState) Depth() int
func (s *StreamState) CurrentPath() string
```

**Convenience API** (handles tokenization):
```go
type StreamDecoder struct {
    reader io.Reader
    state *StreamState
    // Internal tokenization (~200 lines)
}

func (d *StreamDecoder) ReadToken() (token.Token, error)
func (d *StreamDecoder) ReadValue() (*ir.Node, error)
```

**Benefits**:
- ✅ Minimal core (just stack management)
- ✅ Tokenization is optional/internal
- ✅ Can use StreamState directly if you have tokens
- ✅ Still convenient for io.Reader use cases

**Code**:
- StreamState: ~200 lines (just stack/path management)
- StreamDecoder: ~400 lines (tokenization + convenience)
- Total: ~600 lines (vs ~700 if tokenization was separate)

This is the sweet spot: minimal core, convenient wrapper.
