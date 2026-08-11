# Stream Package Design

## Package Structure

Move streaming API to dedicated `stream/` package to avoid cluttering `parse/`.

## Package Location

```
go-tony/
├── parse/          # Existing parsing (unchanged)
├── token/          # Tokenization (unchanged)
├── encode/         # Encoding (unchanged)
├── ir/             # Intermediate representation (unchanged)
└── stream/         # NEW: Streaming encode/decode
    ├── doc.go
    ├── event.go           # Event types
    ├── state.go           # StreamState (minimal core)
    ├── decoder.go         # StreamDecoder
    ├── encoder.go         # StreamEncoder
    ├── decoder_test.go
    ├── encoder_test.go
    └── state_test.go
```

## Package API

### Package: `stream`

```go
package stream

import (
    "io"
    "github.com/signadot/tony-format/go-tony/token"
)
```

## Event Types

### event.go

```go
package stream

// Event represents a structural event from the decoder
type Event struct {
    Type EventType
    
    // Value fields (only one is set based on Type)
    Key      string
    String   string
    Int      int64
    Float    float64
    Bool     bool
}

type EventType int

const (
    EventBeginObject EventType = iota
    EventEndObject
    EventBeginArray
    EventEndArray
    EventKey        // Object key
    EventString     // String value
    EventInt        // Integer value
    EventFloat      // Float value
    EventBool       // Boolean value
    EventNull       // Null value
)

func (t EventType) String() string {
    switch t {
    case EventBeginObject:
        return "BeginObject"
    case EventEndObject:
        return "EndObject"
    case EventBeginArray:
        return "BeginArray"
    case EventEndArray:
        return "EndArray"
    case EventKey:
        return "Key"
    case EventString:
        return "String"
    case EventInt:
        return "Int"
    case EventFloat:
        return "Float"
    case EventBool:
        return "Bool"
    case EventNull:
        return "Null"
    default:
        return "Unknown"
    }
}
```

## StreamState (Minimal Core)

### state.go

```go
package stream

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
func (s *StreamState) ProcessToken(tok token.Token) error

// Queryable State Methods
func (s *StreamState) Depth() int                    // Current nesting depth (0 = top level)
func (s *StreamState) CurrentPath() string           // Current kinded path (e.g., "", "key", "key[0]")
func (s *StreamState) ParentPath() string            // Parent path (one level up)
func (s *StreamState) IsInObject() bool             // True if currently inside an object
func (s *StreamState) IsInArray() bool               // True if currently inside an array
func (s *StreamState) CurrentKey() string            // Current object key (if in object)
func (s *StreamState) CurrentIndex() int              // Current array index (if in array)
func (s *StreamState) Offset() int64                 // Current byte offset (tracks from tokens)
```

## StreamDecoder

### decoder.go

```go
package stream

import (
    "io"
)

// StreamDecoder provides structural event-based decoding.
// Only supports bracketed structures ({...} and [...]).
// Block style (TArrayElt) is not supported.
type StreamDecoder struct {
    // Private fields
    // - reader io.Reader
    // - state *StreamState
    // - Internal tokenization (~200 lines, not exported)
}

// StreamOption configures StreamDecoder behavior.
type StreamOption func(*streamOpts)

// NewStreamDecoder creates a new StreamDecoder reading from r.
func NewStreamDecoder(r io.Reader, opts ...StreamOption) *StreamDecoder

// ReadEvent reads the next structural event from the stream.
// Returns structural events (BeginObject, Key, String, etc.) that correspond
// to the encoder's API. Low-level tokens (commas, colons) are elided.
// Returns io.EOF when stream is exhausted.
func (d *StreamDecoder) ReadEvent() (Event, error)

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

## StreamEncoder

### encoder.go

```go
package stream

import (
    "io"
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

// Control Methods
func (e *StreamEncoder) Flush() error
func (e *StreamEncoder) Reset(w io.Writer, opts ...StreamOption)
```

## Usage Examples

### Example 1: Basic Decoding

```go
import "github.com/signadot/tony-format/go-tony/stream"

dec := stream.NewStreamDecoder(reader)

for {
    event, err := dec.ReadEvent()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    
    switch event.Type {
    case stream.EventBeginObject:
        fmt.Println("Begin object")
    case stream.EventKey:
        fmt.Printf("Key: %s\n", event.Key)
    case stream.EventString:
        fmt.Printf("String: %s\n", event.String)
    case stream.EventEndObject:
        fmt.Println("End object")
    }
}
```

### Example 2: Symmetric Encode/Decode

```go
import "github.com/signadot/tony-format/go-tony/stream"

// Encoding
enc := stream.NewStreamEncoder(writer)
enc.BeginObject()
enc.WriteKey("name")
enc.WriteString("value")
enc.EndObject()

// Decoding (symmetric!)
dec := stream.NewStreamDecoder(reader)
event, _ := dec.ReadEvent()  // EventBeginObject
event, _ := dec.ReadEvent()  // EventKey("name")
event, _ := dec.ReadEvent()  // EventString("value")
event, _ := dec.ReadEvent()  // EventEndObject
```

### Example 3: Indexing with Range Descriptors

```go
import "github.com/signadot/tony-format/go-tony/stream"

dec := stream.NewStreamDecoder(chunkReader)
enc := stream.NewStreamEncoder(newSnapshotWriter)
indexBuilder := NewIndexBuilder()

var rangeState *RangeState

for {
    event, err := dec.ReadEvent()
    if err == io.EOF {
        break
    }
    
    // Process event and write to new snapshot
    switch event.Type {
    case stream.EventBeginObject:
        enc.BeginObject()
        
    case stream.EventEndObject:
        enc.EndObject()
        
    case stream.EventBeginArray:
        enc.BeginArray()
        if rangeState == nil {
            rangeState = &RangeState{
                StartIndex:  enc.CurrentIndex(),
                StartOffset: enc.Offset(),
            }
        }
        
    case stream.EventEndArray:
        enc.EndArray()
        if rangeState != nil {
            finalizeRange(rangeState, enc, indexBuilder)
            rangeState = nil
        }
        
    case stream.EventKey:
        enc.WriteKey(event.Key)
        
    case stream.EventString:
        enc.WriteString(event.String)
        trackRange(enc, &rangeState, indexBuilder)
        
    case stream.EventInt:
        enc.WriteInt(event.Int)
        trackRange(enc, &rangeState, indexBuilder)
        
    case stream.EventFloat:
        enc.WriteFloat(event.Float)
        trackRange(enc, &rangeState, indexBuilder)
        
    case stream.EventBool:
        enc.WriteBool(event.Bool)
        trackRange(enc, &rangeState, indexBuilder)
        
    case stream.EventNull:
        enc.WriteNull()
        trackRange(enc, &rangeState, indexBuilder)
    }
}
```

## Package Dependencies

```
stream/
├── Depends on:
│   ├── token/     (for tokenization)
│   └── io         (standard library)
│
└── Used by:
    └── snap/      (for snapshot indexing)
```

## Migration from parse Package

### Old (if it existed)
```go
import "github.com/signadot/tony-format/go-tony/parse"

dec := parse.NewStreamDecoder(reader)
tok, _ := dec.ReadToken()
```

### New
```go
import "github.com/signadot/tony-format/go-tony/stream"

dec := stream.NewStreamDecoder(reader)
event, _ := dec.ReadEvent()
```

## Benefits of Dedicated Package

1. ✅ **Separation of concerns**: Streaming API separate from parsing
2. ✅ **Cleaner parse package**: Parse package stays focused on parsing
3. ✅ **Clear purpose**: `stream/` package is clearly for streaming encode/decode
4. ✅ **Better organization**: Related functionality grouped together

## File Organization

```
stream/
├── doc.go              # Package documentation
├── event.go            # Event types and constants
├── state.go            # StreamState (minimal core, ~200 lines)
├── decoder.go          # StreamDecoder (~400 lines)
├── encoder.go          # StreamEncoder (~400-500 lines)
├── decoder_test.go     # Decoder tests
├── encoder_test.go     # Encoder tests
└── state_test.go       # State tests
```

## Summary

**New Package**: `stream/`

**Components**:
- `Event` types - structural events
- `StreamState` - minimal core for state management
- `StreamDecoder` - structural event-based decoding
- `StreamEncoder` - structural encoding

**Benefits**:
- Clean separation from `parse/`
- Symmetric encode/decode API
- Structural events (no low-level tokens)
- Perfect for indexing use cases
