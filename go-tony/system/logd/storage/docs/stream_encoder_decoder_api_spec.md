# API Specification: parse.StreamEncoder and parse.StreamDecoder

## Overview

This document specifies the exact API for `parse.StreamEncoder` and `parse.StreamDecoder`, providing explicit stack management for streaming Tony documents.

## Key Simplifications

1. **Bracketed-Only**: Only supports bracketed structures (`{...}` and `[...]`), not block style
   - Eliminates `TArrayElt` handling complexity
   - Makes path tracking reliable and accurate
   - Simplifies state machine significantly

2. **token.Reader**: Uses simpler `token.Reader` instead of `token.Source`
   - `token.Reader` does just tokenization (no path tracking)
   - Path tracking implemented in `parse.StreamDecoder`
   - Cleaner separation: tokenization vs. streaming

3. **Sparse Arrays as Objects**: Sparse arrays use `BeginObject()` in streaming layer
   - Structural distinction (object vs array) handled at streaming layer
   - Semantic distinction (sparse array vs object) handled at parsing layer
   - Higher layers determine `!sparsearray` tag based on key types

## Package Structure

```
parse/
  stream_encoder.go      # StreamEncoder implementation
  stream_decoder.go       # StreamDecoder implementation
  stream_state.go         # Shared state management (stateMachine, pathStack, nameStack)
  stream_opts.go          # Options and configuration
  stream_encoder_test.go  # Encoder tests
  stream_decoder_test.go  # Decoder tests
  stream_state_test.go    # State management tests
```

## StreamEncoder API

### Type Definitions

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
    // Private fields - not exported
}

// NewStreamEncoder creates a new StreamEncoder writing to w.
func NewStreamEncoder(w io.Writer, opts ...StreamOption) *StreamEncoder

// StreamOption configures StreamEncoder behavior.
type StreamOption func(*streamOpts)

// streamOpts holds configuration for StreamEncoder.
type streamOpts struct {
    format format.Format  // Tony, YAML, JSON
    indent string         // Indentation string (default: "  ")
    multiline bool        // Use multiline format
    // ... other options
}
```

### Queryable State Methods

```go
// Depth returns the current nesting depth (0 = top level).
func (e *StreamEncoder) Depth() int

// CurrentPath returns the current kinded path from root.
// Returns "" for root, "key" for object key, "key[0]" for array element, etc.
func (e *StreamEncoder) CurrentPath() string

// ParentPath returns the parent path (one level up).
// Returns "" if at root level.
func (e *StreamEncoder) ParentPath() string

// IsInObject returns true if currently inside an object.
func (e *StreamEncoder) IsInObject() bool

// IsInArray returns true if currently inside an array.
func (e *StreamEncoder) IsInArray() bool

// CurrentKey returns the current object key (if in object context).
// Returns "" if not in object or no key set yet.
func (e *StreamEncoder) CurrentKey() string

// CurrentIndex returns the current array index (if in array context).
// Returns -1 if not in array.
func (e *StreamEncoder) CurrentIndex() int

// Offset returns the current absolute byte offset in the output stream.
func (e *StreamEncoder) Offset() int64
```

### Structure Control Methods

```go
// BeginObject starts a new object.
// Must be followed by key-value pairs and EndObject().
// Note: Sparse arrays also use BeginObject() (semantic distinction at parse layer).
func (e *StreamEncoder) BeginObject() error

// EndObject ends the current object.
// Must be called after BeginObject() and all key-value pairs.
func (e *StreamEncoder) EndObject() error

// BeginArray starts a new array.
// Must be followed by values and EndArray().
// Only for regular arrays ([...]), not sparse arrays.
func (e *StreamEncoder) BeginArray() error

// EndArray ends the current array.
// Must be called after BeginArray() and all values.
func (e *StreamEncoder) EndArray() error
```

### Value Writing Methods

```go
// WriteKey writes an object key.
// Must be called before writing the value in object context.
func (e *StreamEncoder) WriteKey(key string) error

// WriteString writes a string value.
func (e *StreamEncoder) WriteString(value string) error

// WriteInt writes an integer value.
func (e *StreamEncoder) WriteInt(value int64) error

// WriteFloat writes a float value.
func (e *StreamEncoder) WriteFloat(value float64) error

// WriteBool writes a boolean value.
func (e *StreamEncoder) WriteBool(value bool) error

// WriteNull writes a null value.
func (e *StreamEncoder) WriteNull() error
```

### Token-Based Writing

```go
// WriteToken writes a token to the stream.
// Updates state automatically based on token type.
// For structural tokens (BeginObject, EndObject, etc.), use structure methods instead.
// Returns error if TArrayElt (block style) is encountered (not supported).
func (e *StreamEncoder) WriteToken(tok token.Token) error

// WriteTokens writes multiple tokens to the stream.
// Returns error if TArrayElt (block style) is encountered (not supported).
func (e *StreamEncoder) WriteTokens(tokens []token.Token) error
```

### Control Methods

```go
// Flush flushes any buffered data to the underlying writer.
func (e *StreamEncoder) Flush() error

// Reset resets the encoder to write to a new writer.
func (e *StreamEncoder) Reset(w io.Writer, opts ...StreamOption)
```

## StreamDecoder API

### Type Definitions

```go
// StreamDecoder provides explicit stack management for streaming Tony document decoding.
// Only supports bracketed structures ({...} and [...]).
// Block style (TArrayElt) is not supported.
type StreamDecoder struct {
    // Private fields - not exported
}

// NewStreamDecoder creates a new StreamDecoder reading from r.
// Uses token.Reader internally for tokenization (not token.Source).
func NewStreamDecoder(r io.Reader, opts ...StreamOption) *StreamDecoder
```

### Queryable State Methods

```go
// Depth returns the current nesting depth (0 = top level).
func (d *StreamDecoder) Depth() int

// CurrentPath returns the current kinded path from root.
func (d *StreamDecoder) CurrentPath() string

// ParentPath returns the parent path (one level up).
func (d *StreamDecoder) ParentPath() string

// IsInObject returns true if currently inside an object.
func (d *StreamDecoder) IsInObject() bool

// IsInArray returns true if currently inside an array.
func (d *StreamDecoder) IsInArray() bool

// CurrentKey returns the current object key (if in object context).
func (d *StreamDecoder) CurrentKey() string

// CurrentIndex returns the current array index (if in array context).
func (d *StreamDecoder) CurrentIndex() int

// Offset returns the current absolute byte offset in the input stream.
func (d *StreamDecoder) Offset() int64
```

### Reading Methods

```go
// ReadToken reads the next token from the stream.
// Updates state automatically based on token type.
// Returns error if TArrayElt (block style) is encountered (not supported).
// Returns io.EOF when stream is exhausted.
func (d *StreamDecoder) ReadToken() (token.Token, error)

// ReadValue reads a complete value (node) from the stream.
// Returns a complete ir.Node representing the value.
// Returns io.EOF when stream is exhausted.
func (d *StreamDecoder) ReadValue() (*ir.Node, error)

// PeekToken returns the next token without consuming it.
// Subsequent ReadToken() calls will return the same token.
func (d *StreamDecoder) PeekToken() (token.Token, error)
```

### Control Methods

```go
// Reset resets the decoder to read from a new reader.
func (d *StreamDecoder) Reset(r io.Reader, opts ...StreamOption)
```

## State Management Details

### stateMachine

```go
// stateMachine tracks structural depth and current structure type.
// Simplified: Only bracketed structures, no block style.
type stateMachine struct {
    depth int           // Current nesting depth
    stack []structureType // Stack of structure types
}

type structureType int

const (
    structureNone structureType = iota
    structureObject  // { ... } - includes sparse arrays (semantic distinction at parse layer)
    structureArray   // [ ... ]
)

// Methods (internal)
func (sm *stateMachine) push(typ structureType)
func (sm *stateMachine) pop() structureType
func (sm *stateMachine) reset()
func (sm *stateMachine) current() structureType
```

### pathStack

```go
// pathStack tracks kinded paths explicitly.
// Simplified: Only bracketed structures, no block style (TArrayElt).
type pathStack struct {
    current string   // Current kinded path
    stack   []string // Stack of parent paths
    names   []string // Stack of object keys
    indices []int    // Stack of array indices
}

// Methods (internal)
func (ps *pathStack) push()
func (ps *pathStack) pop()
func (ps *pathStack) setKey(key string)  // For object keys (including sparse array indices)
func (ps *pathStack) incrementIndex()    // For array indices
func (ps *pathStack) reset()
```

### nameStack

```go
// nameStack tracks object keys for path construction.
type nameStack struct {
    stack []string // Stack of object keys
}

// Methods (internal)
func (ns *nameStack) push(key string)
func (ns *nameStack) pop() string
func (ns *nameStack) current() string
func (ns *nameStack) reset()
```

## Usage Examples

### Basic Encoding

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

### Encoding with Path Tracking (for Indexing)

```go
enc := parse.NewStreamEncoder(writer)
var index []IndexEntry

enc.BeginObject()
for key, value := range data {
    enc.WriteKey(key)
    path := enc.CurrentPath()      // e.g., "key"
    offset := enc.Offset()          // Byte offset
    
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

### Basic Decoding

```go
dec := parse.NewStreamDecoder(reader)

for {
    tok, err := dec.ReadToken()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    
    // Process token
    switch tok.Type {
    case token.BeginObject:
        // Handle object start
    case token.EndObject:
        // Handle object end
    // ... etc
    }
}
```

### Decoding with Path Tracking (for Indexing)

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
    
    // Track path boundaries
    if dec.IsInObject() && tok.Type == token.TColon {
        // Object key boundary
        key := dec.CurrentKey()
        path := dec.CurrentPath()
        offset := dec.Offset()
        
        index = append(index, IndexEntry{
            Path: path,
            Offset: offset,
            Key: key,
        })
    }
}
```

### Value-Based Decoding

```go
dec := parse.NewStreamDecoder(reader)

for {
    node, err := dec.ReadValue()
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

## Error Handling

### Common Errors

```go
// Structure errors
var ErrUnmatchedEnd = errors.New("unmatched end structure")
var ErrUnexpectedEnd = errors.New("unexpected end structure")
var ErrInvalidState = errors.New("invalid encoder/decoder state")
var ErrBlockStyleNotSupported = errors.New("block style (TArrayElt) not supported in streaming")

// I/O errors
// Wrapped io.EOF, io.ErrUnexpectedEOF, etc.
```

### Error Examples

```go
// Invalid structure nesting
enc.BeginObject()
enc.EndArray()  // Error: ErrInvalidState

// Unmatched end
enc.BeginObject()
enc.EndObject()
enc.EndObject()  // Error: ErrUnmatchedEnd

// Invalid key writing
enc.BeginArray()
enc.WriteKey("key")  // Error: ErrInvalidState (not in object)

// Block style not supported
enc.WriteToken(token.Token{Type: token.TArrayElt})  // Error: ErrBlockStyleNotSupported
```

## Implementation Notes

### State Invariants

1. **Depth Invariant**: `stateMachine.depth == len(pathStack.stack)`
2. **Path Invariant**: `pathStack.current` reflects current position
3. **Key Invariant**: `nameStack.current()` matches current object key (if in object)
4. **Index Invariant**: `pathStack.indices[len(pathStack.indices)-1]` matches current array index (if in array)

### Offset Tracking

- **Encoder**: Tracks bytes written to `io.Writer`
- **Decoder**: Tracks bytes read from `io.Reader`
- **Accuracy**: Offsets are accurate to byte level
- **Performance**: Offset tracking is O(1) per operation

### Path Format

- **Root**: `""` (empty string)
- **Object Key**: `"key"` or `"key.subkey"`
- **Array Index**: `"key[0]"` or `"[0]"`
- **Sparse Array**: `"key{5}"` or `"{5}"` (treated as object key in streaming layer)
- **Nested**: `"key[0].subkey"` or `"key.subkey[1]"`

**Note**: Sparse arrays use `{index}` syntax in paths, but are created using `BeginObject()` in the streaming API. The semantic distinction (sparse array vs object) is determined at the parsing layer based on key types.

### Formatting

- **Indentation**: Configurable (default: 2 spaces)
- **Spacing**: Automatic spacing around colons, commas
- **Multiline**: Optional multiline format for objects/arrays
- **Comments**: Preserved during encoding/decoding
- **Block Style**: Not supported (only bracketed structures)

## Testing Requirements

### Unit Tests

1. **State Management**:
   - Stack push/pop operations (bracketed structures only)
   - Path tracking correctness (bracketed structures only)
   - Depth tracking
   - Invariant checks
   - Sparse array handling (as objects)

2. **Encoder**:
   - Basic encoding (all value types)
   - Structure nesting (bracketed structures only)
   - Path tracking (bracketed structures only)
   - Offset tracking
   - Formatting
   - Error handling (TArrayElt rejection)

3. **Decoder**:
   - Basic decoding (all value types)
   - Structure nesting (bracketed structures only)
   - Path tracking (bracketed structures only)
   - Offset tracking
   - Peek operations
   - Error handling (TArrayElt rejection)

### Integration Tests

1. **Round-Trip**:
   - Encode → Decode → Verify
   - Path tracking consistency
   - Offset accuracy

2. **Compatibility**:
   - StreamEncoder output matches `Parse()` output
   - StreamDecoder output matches `Parse()` output

## Performance Considerations

### Encoder

- **Buffer Management**: Buffers writes for efficiency
- **Allocation**: Minimize allocations in hot paths
- **Formatting**: Lazy formatting (format on flush)

### Decoder

- **Token Source**: Reuses `token.TokenSource` (efficient buffering)
- **State Updates**: O(1) state updates per token
- **Path Tracking**: O(1) path updates per token

## Migration from token.{Sink,Source}

### Key Differences

1. **Explicit vs Implicit**: State is queryable, not hidden
2. **Structure Methods**: `BeginObject/EndObject` vs automatic detection
3. **Path Tracking**: Explicit path stack vs internal tracking
4. **Offset Tracking**: Separate from token stream
5. **Bracketed-Only**: Only supports bracketed structures (no block style)
6. **token.Reader**: Uses simpler `token.Reader` instead of `token.Source`

### Migration Guide

```go
// Old: token.TokenSink
sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
    // Index building
})

// New: parse.StreamEncoder (bracketed-only)
enc := parse.NewStreamEncoder(writer)
// Build index explicitly using queryable state
path := enc.CurrentPath()
offset := enc.Offset()

// Old: token.TokenSource
source := token.NewTokenSource(reader)
tokens, err := source.Read()

// New: parse.StreamDecoder (uses token.Reader internally)
dec := parse.NewStreamDecoder(reader)
tok, err := dec.ReadToken()
path := dec.CurrentPath()  // Queryable state
```

### Block Style Limitation

**Old**: `token.TokenSink` attempted to handle block style (TArrayElt) but path tracking didn't work reliably.

**New**: `parse.StreamEncoder/Decoder` only support bracketed structures. Block style is not supported.

**Workaround**: Convert block style to bracketed style before streaming, or use full `parse.Parse()` for block style documents.

## Future Enhancements

1. **Streaming Validation**: Validate during encode/decode
2. **Streaming Transformation**: Transform during encode/decode
3. **Streaming Merge**: Merge multiple streams
4. **Index Helpers**: Built-in index building utilities
5. **Block Style Support**: Optional block style support (if needed, but adds complexity)

## Simplifications Summary

### Bracketed-Only Benefits
- ✅ **~100+ lines eliminated**: No `TArrayElt` handling
- ✅ **Reliable path tracking**: Explicit boundaries make tracking accurate
- ✅ **Simple state machine**: Just push/pop on brackets
- ✅ **Matches jsontext**: All bracketed, proven pattern

### token.Reader Benefits
- ✅ **Cleaner separation**: Tokenization vs. streaming
- ✅ **Simpler token package**: Just tokenization, no path tracking
- ✅ **Path tracking where used**: In parse package where it's needed

### Sparse Arrays as Objects Benefits
- ✅ **Simpler API**: Just `BeginObject()` vs. separate sparse array methods
- ✅ **Semantic distinction**: Handled at parse layer where it belongs
- ✅ **Matches reality**: Structurally objects, semantically arrays
