# Final Streaming API Specification

## Overview

After all simplifications, the final API consists of:
1. **StreamState** - Minimal core (just stack/path management, ~200 lines)
2. **StreamDecoder** - Convenience wrapper (adds tokenization for io.Reader, ~400 lines)
3. **StreamEncoder** - Encoding with explicit stack management (~400-500 lines)

## StreamState API (Minimal Core)

```go
package parse

import "github.com/signadot/tony-format/go-tony/token"

// StreamState provides minimal stack/state/path management.
// Just processes tokens and tracks state - no tokenization, no io.Reader.
// Use this if you already have tokens.
type StreamState struct {
    // Private fields
}

// NewStreamState creates a new StreamState for tracking structure state.
func NewStreamState() *StreamState

// ProcessToken processes a token and updates state/path tracking.
// Call this for each token in order.
func (s *StreamState) ProcessToken(tok token.Token) error

// Queryable State Methods
func (s *StreamState) Depth() int                    // Current nesting depth (0 = top level)
func (s *StreamState) CurrentPath() string           // Current kinded path (e.g., "", "key", "key[0]")
func (s *StreamState) ParentPath() string            // Parent path (one level up)
func (s *StreamState) IsInObject() bool              // True if currently inside an object
func (s *StreamState) IsInArray() bool               // True if currently inside an array
func (s *StreamState) CurrentKey() string            // Current object key (if in object)
func (s *StreamState) CurrentIndex() int             // Current array index (if in array)
func (s *StreamState) Offset() int64                 // Current byte offset (tracks from tokens)
```

## StreamDecoder API (Convenience Wrapper)

```go
package parse

import (
    "io"
    "github.com/signadot/tony-format/go-tony/ir"
    "github.com/signadot/tony-format/go-tony/token"
)

// StreamDecoder provides convenience wrapper with tokenization for io.Reader.
// Uses StreamState internally, adds tokenization for streaming from io.Reader.
// Only supports bracketed structures ({...} and [...]).
type StreamDecoder struct {
    // Private fields
    // - reader io.Reader
    // - state *StreamState
    // - Internal tokenization (~200 lines, not exported)
}

// NewStreamDecoder creates a new StreamDecoder reading from r.
func NewStreamDecoder(r io.Reader, opts ...StreamOption) *StreamDecoder

// StreamOption configures StreamDecoder behavior.
type StreamOption func(*streamOpts)

// ReadToken reads the next token from the stream.
// Tokenizes internally, updates state automatically.
// Returns error if TArrayElt (block style) is encountered (not supported).
// Returns io.EOF when stream is exhausted.
func (d *StreamDecoder) ReadToken() (token.Token, error)

// ProcessToken processes a token for pre-tokenized data.
// Updates state/path tracking. Use this if you already have tokens.
// Delegates to internal StreamState.
func (d *StreamDecoder) ProcessToken(tok token.Token) error

// ReadValue reads a complete value (ir.Node) from the stream.
// Replaces NodeParser.ParseNext() functionality.
// Returns io.EOF when stream is exhausted.
func (d *StreamDecoder) ReadValue() (*ir.Node, error)

// PeekToken returns the next token without consuming it.
func (d *StreamDecoder) PeekToken() (token.Token, error)

// Queryable State Methods (delegate to internal StreamState)
func (d *StreamDecoder) Depth() int
func (d *StreamDecoder) CurrentPath() string
func (d *StreamDecoder) ParentPath() string
func (d *StreamDecoder) IsInObject() bool
func (d *StreamDecoder) IsInArray() bool
func (d *StreamDecoder) CurrentKey() string
func (d *StreamDecoder) CurrentIndex() int
func (d *StreamDecoder) Offset() int64

// Reset resets the decoder to read from a new reader.
func (d *StreamDecoder) Reset(r io.Reader, opts ...StreamOption)
```

## StreamEncoder API

```go
package parse

import (
    "io"
    "github.com/signadot/tony-format/go-tony/token"
)

// StreamEncoder provides explicit stack management for streaming Tony document encoding.
// Only supports bracketed structures ({...} and [...]).
// Block style (TArrayElt) is not supported.
type StreamEncoder struct {
    // Private fields
}

// NewStreamEncoder creates a new StreamEncoder writing to w.
func NewStreamEncoder(w io.Writer, opts ...StreamOption) *StreamEncoder

// Queryable State Methods
func (e *StreamEncoder) Depth() int
func (e *StreamEncoder) CurrentPath() string
func (e *StreamEncoder) ParentPath() string
func (e *StreamEncoder) IsInObject() bool
func (e *StreamEncoder) IsInArray() bool
func (e *StreamEncoder) CurrentKey() string
func (e *StreamEncoder) CurrentIndex() int
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
// Returns error if TArrayElt (block style) is encountered (not supported).
func (e *StreamEncoder) WriteToken(tok token.Token) error
func (e *StreamEncoder) WriteTokens(tokens []token.Token) error

// Control Methods
func (e *StreamEncoder) Flush() error
func (e *StreamEncoder) Reset(w io.Writer, opts ...StreamOption)
```

## Usage Examples

### Example 1: Basic Decoding (Replaces NodeParser)

```go
// Old: NodeParser
// source := token.NewTokenSource(reader)
// parser := parse.NewNodeParser(source)
// node, err := parser.ParseNext()

// New: StreamDecoder
dec := parse.NewStreamDecoder(reader)
for {
    node, err := dec.ReadValue()  // Same functionality as ParseNext()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    // Process complete value
    processNode(node)
}
```

### Example 2: Token-Based Decoding

```go
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
```

### Example 3: Path Tracking for Indexing

```go
dec := parse.NewStreamDecoder(reader)
var index []IndexEntry

for {
    tok, err := dec.ReadToken()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    
    // Query state at any time
    path := dec.CurrentPath()   // e.g., "key", "key[0]"
    depth := dec.Depth()         // Current nesting depth
    offset := dec.Offset()       // Byte offset
    
    // Build index at boundaries
    if dec.IsInObject() && tok.Type == token.TColon {
        key := dec.CurrentKey()
        index = append(index, IndexEntry{
            Path: path,
            Key: key,
            Offset: offset,
        })
    }
}
```

### Example 4: Using StreamState Directly (Pre-Tokenized Data)

```go
// If you already have tokens, use StreamState directly
state := parse.NewStreamState()
tokens, err := token.Tokenize(nil, data)  // Pre-tokenized
if err != nil {
    return err
}

for _, tok := range tokens {
    if err := state.ProcessToken(tok); err != nil {
        return err
    }
    
    // Query state
    path := state.CurrentPath()
    depth := state.Depth()
    // Build index, etc.
}
```

### Example 5: Basic Encoding

```go
enc := parse.NewStreamEncoder(writer)
defer enc.Flush()

enc.BeginObject()
enc.WriteKey("name")
enc.WriteString("value")
enc.WriteKey("array")
enc.BeginArray()
enc.WriteInt(1)
enc.WriteInt(2)
enc.EndArray()
enc.EndObject()
```

### Example 6: Encoding with Path Tracking (for Indexing)

```go
enc := parse.NewStreamEncoder(writer)
var index []IndexEntry

enc.BeginObject()
for key, value := range data {
    enc.WriteKey(key)
    
    // Query state at any time
    path := enc.CurrentPath()   // e.g., "key"
    offset := enc.Offset()       // Byte offset
    
    // Build index entry
    index = append(index, IndexEntry{
        Path: path,
        Offset: offset,
        ParentPath: enc.ParentPath(),
    })
    
    enc.WriteString(value)
}
enc.EndObject()
```

### Example 7: Token-Based Encoding

```go
enc := parse.NewStreamEncoder(writer)
tokens := []token.Token{
    token.BeginObject(),
    token.String("key"),
    token.Colon(),
    token.String("value"),
    token.EndObject(),
}

for _, tok := range tokens {
    if err := enc.WriteToken(tok); err != nil {
        return err
    }
}
enc.Flush()
```

## Key Design Decisions

### 1. Two-Level Design
- **StreamState**: Minimal core (~200 lines) - just stack/path management
- **StreamDecoder**: Convenience wrapper (~400 lines) - adds tokenization for io.Reader
- **Flexibility**: Use StreamState directly if you have tokens

### 2. Bracketed-Only
- Only supports `{...}` and `[...]` structures
- Block style (`TArrayElt`) returns error
- Eliminates ~100+ lines of complexity

### 3. Sparse Arrays as Objects
- Use `BeginObject()` for sparse arrays
- Semantic distinction (sparse array vs object) handled at parse layer
- Simpler API

### 4. Explicit Stack Management
- All state is queryable (depth, path, structure type)
- Explicit `BeginObject/Array`, `EndObject/Array` operations
- Matches jsontext pattern

### 5. Tokenization is Internal
- StreamDecoder handles tokenization internally (~200 lines)
- Not exported, not a separate type
- Can use StreamState directly if you have tokens

## Error Handling

```go
var (
    ErrUnmatchedEnd           = errors.New("unmatched end structure")
    ErrUnexpectedEnd          = errors.New("unexpected end structure")
    ErrInvalidState           = errors.New("invalid encoder/decoder state")
    ErrBlockStyleNotSupported = errors.New("block style (TArrayElt) not supported in streaming")
)
```

## Code Size

**Eliminated**: ~2,427 lines
- `parse/incremental.go` + tests (~1,282 lines)
- `token/sink.go` + tests (~542 lines)
- `token/source.go` + tests (~603 lines)

**Added**: ~1,000-1,300 lines
- `parse/stream_state.go` + tests (~200 lines) - minimal core
- `parse/stream_decoder.go` + tests (~400 lines) - convenience wrapper
- `parse/stream_encoder.go` + tests (~400-500 lines)

**Net Reduction**: ~1,100-1,400 lines (45-55% reduction!)

## Migration Path

### From NodeParser
```go
// Old
source := token.NewTokenSource(reader)
parser := parse.NewNodeParser(source)
node, err := parser.ParseNext()

// New
dec := parse.NewStreamDecoder(reader)
node, err := dec.ReadValue()  // Same functionality
```

### From TokenSink
```go
// Old
sink := token.NewTokenSink(writer, callback)

// New
enc := parse.NewStreamEncoder(writer)
// Build index explicitly using queryable state
path := enc.CurrentPath()
offset := enc.Offset()
```

### From TokenSource
```go
// Old
source := token.NewTokenSource(reader)
tokens, err := source.Read()

// New
dec := parse.NewStreamDecoder(reader)
tok, err := dec.ReadToken()  // Tokenizes internally
path := dec.CurrentPath()    // Queryable state
```
