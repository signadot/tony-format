# Structural Decode API Design

## The Idea

Instead of returning low-level `token.Token` values (commas, colons, brackets), the decoder should return **structural events** that correspond to the encoder's API.

## Encoder API (Reference)

```go
// Structure Control
enc.BeginObject()
enc.EndObject()
enc.BeginArray()
enc.EndArray()

// Value Writing
enc.WriteKey(key string)
enc.WriteString(value string)
enc.WriteInt(value int64)
enc.WriteFloat(value float64)
enc.WriteBool(value bool)
enc.WriteNull()
```

## Proposed Decoder API

### Structural Events

```go
package parse

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
```

### StreamDecoder API

```go
type StreamDecoder struct {
    // Private: reader, tokenization, internal StreamState
}

// NewStreamDecoder creates a new decoder reading from r
func NewStreamDecoder(r io.Reader, opts ...StreamOption) *StreamDecoder

// ReadEvent reads the next structural event
// Returns io.EOF when stream is exhausted
func (d *StreamDecoder) ReadEvent() (Event, error)

// Queryable State Methods (same as before)
func (d *StreamDecoder) Depth() int
func (d *StreamDecoder) CurrentPath() string
func (d *StreamDecoder) ParentPath() string
func (d *StreamDecoder) IsInObject() bool
func (d *StreamDecoder) IsInArray() bool
func (d *StreamDecoder) CurrentKey() string
func (d *StreamDecoder) CurrentIndex() int
func (d *StreamDecoder) Offset() int64

// Reset resets decoder to read from new reader
func (d *StreamDecoder) Reset(r io.Reader, opts ...StreamOption)
```

## Usage Examples

### Example 1: Basic Reading

```go
dec := parse.NewStreamDecoder(reader)

for {
    event, err := dec.ReadEvent()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    
    switch event.Type {
    case parse.EventBeginObject:
        fmt.Println("Begin object")
    case parse.EventEndObject:
        fmt.Println("End object")
    case parse.EventBeginArray:
        fmt.Println("Begin array")
    case parse.EventEndArray:
        fmt.Println("End array")
    case parse.EventKey:
        fmt.Printf("Key: %s\n", event.Key)
    case parse.EventString:
        fmt.Printf("String: %s\n", event.String)
    case parse.EventInt:
        fmt.Printf("Int: %d\n", event.Int)
    case parse.EventFloat:
        fmt.Printf("Float: %f\n", event.Float)
    case parse.EventBool:
        fmt.Printf("Bool: %v\n", event.Bool)
    case parse.EventNull:
        fmt.Println("Null")
    }
}
```

### Example 2: Symmetric Encode/Decode

```go
// Encoding
enc := parse.NewStreamEncoder(writer)
enc.BeginObject()
enc.WriteKey("name")
enc.WriteString("value")
enc.WriteKey("count")
enc.WriteInt(42)
enc.EndObject()

// Decoding (symmetric!)
dec := parse.NewStreamDecoder(reader)
event, _ := dec.ReadEvent()  // EventBeginObject
event, _ := dec.ReadEvent()  // EventKey("name")
event, _ := dec.ReadEvent()  // EventString("value")
event, _ := dec.ReadEvent()  // EventKey("count")
event, _ := dec.ReadEvent()  // EventInt(42)
event, _ := dec.ReadEvent()  // EventEndObject
```

### Example 3: Indexing with Range Descriptors

```go
dec := parse.NewStreamDecoder(chunkReader)
enc := parse.NewStreamEncoder(newSnapshotWriter)
indexBuilder := NewIndexBuilder()

var rangeState *RangeState

for {
    event, err := dec.ReadEvent()
    if err == io.EOF {
        break
    }
    
    // Process event and write to new snapshot
    switch event.Type {
    case parse.EventBeginObject:
        enc.BeginObject()
    case parse.EventEndObject:
        enc.EndObject()
    case parse.EventBeginArray:
        enc.BeginArray()
        // Track range start
        if rangeState == nil {
            rangeState = &RangeState{
                StartIndex:  enc.CurrentIndex(),
                StartOffset: enc.Offset(),
            }
        }
    case parse.EventEndArray:
        enc.EndArray()
        // Finalize range if needed
        if rangeState != nil {
            finalizeRange(rangeState, enc, indexBuilder)
        }
    case parse.EventKey:
        enc.WriteKey(event.Key)
    case parse.EventString:
        enc.WriteString(event.String)
        // Track for range
        trackRange(enc, &rangeState, indexBuilder)
    case parse.EventInt:
        enc.WriteInt(event.Int)
        trackRange(enc, &rangeState, indexBuilder)
    // ... other types
    }
}
```

## Implementation

### Internal Token Processing

```go
func (d *StreamDecoder) ReadEvent() (Event, error) {
    for {
        tok, err := d.readTokenInternal()
        if err != nil {
            return Event{}, err
        }
        
        // Skip structural tokens (commas, colons, brackets)
        switch tok.Type {
        case token.TComma, token.TColon:
            continue  // Skip, these are implicit in events
        
        case token.TLCurl:
            d.state.ProcessToken(tok)
            return Event{Type: EventBeginObject}, nil
        
        case token.TRCurl:
            d.state.ProcessToken(tok)
            return Event{Type: EventEndObject}, nil
        
        case token.TLSquare:
            d.state.ProcessToken(tok)
            return Event{Type: EventBeginArray}, nil
        
        case token.TRSquare:
            d.state.ProcessToken(tok)
            return Event{Type: EventEndArray}, nil
        
        case token.TString:
            d.state.ProcessToken(tok)
            // Check if this is a key or value
            if d.state.IsInObject() && d.state.CurrentKey() == "" {
                // This is a key
                return Event{Type: EventKey, Key: tok.String}, nil
            } else {
                // This is a value
                return Event{Type: EventString, String: tok.String}, nil
            }
        
        case token.TNumber:
            d.state.ProcessToken(tok)
            if tok.Int64 != nil {
                return Event{Type: EventInt, Int: *tok.Int64}, nil
            } else {
                return Event{Type: EventFloat, Float: *tok.Float64}, nil
            }
        
        case token.TBool:
            d.state.ProcessToken(tok)
            return Event{Type: EventBool, Bool: tok.Bool}, nil
        
        case token.TNull:
            d.state.ProcessToken(tok)
            return Event{Type: EventNull}, nil
        
        default:
            // Unknown token type
            return Event{}, fmt.Errorf("unexpected token: %v", tok.Type)
        }
    }
}
```

## Benefits

### 1. Symmetry with Encoder

**Encoder**:
```go
enc.BeginObject()
enc.WriteKey("name")
enc.WriteString("value")
enc.EndObject()
```

**Decoder** (symmetric):
```go
event, _ := dec.ReadEvent()  // BeginObject
event, _ := dec.ReadEvent()  // Key("name")
event, _ := dec.ReadEvent()  // String("value")
event, _ := dec.ReadEvent()  // EndObject
```

### 2. Simpler API

**Before** (token-based):
```go
tok, _ := dec.ReadToken()  // TLCurl
tok, _ := dec.ReadToken()  // TString("name")
tok, _ := dec.ReadToken()  // TColon
tok, _ := dec.ReadToken()  // TString("value")
tok, _ := dec.ReadToken()  // TRCurl
```

**After** (event-based):
```go
event, _ := dec.ReadEvent()  // BeginObject
event, _ := dec.ReadEvent()  // Key("name")
event, _ := dec.ReadEvent()  // String("value")
event, _ := dec.ReadEvent()  // EndObject
```

### 3. Hides Low-Level Details

- No need to handle commas, colons
- No need to distinguish between structural tokens
- Focus on structure, not syntax

### 4. Better for Indexing

```go
// Clear structure boundaries
event, _ := dec.ReadEvent()
if event.Type == parse.EventBeginArray {
    rangeStart := enc.Offset()
    // Track range...
}
```

## Alternative: Keep Token API as Well?

**Question**: Should we keep `ReadToken()` for advanced use cases?

**Option 1**: Only Event API
- ✅ Simpler, cleaner
- ❌ Can't access low-level tokens if needed

**Option 2**: Both APIs
- ✅ Flexibility
- ⚠️ More API surface
- ⚠️ Two ways to do the same thing

**Recommendation**: **Start with Event API only**
- Can add `ReadToken()` later if needed
- Most use cases don't need low-level tokens
- Simpler API is better

## Updated API Summary

```go
// StreamDecoder: Structural event-based decoding
type StreamDecoder struct {
    // ...
}

// ReadEvent reads next structural event
func (d *StreamDecoder) ReadEvent() (Event, error)

// Queryable State Methods
func (d *StreamDecoder) Depth() int
func (d *StreamDecoder) CurrentPath() string
func (d *StreamDecoder) ParentPath() string
func (d *StreamDecoder) IsInObject() bool
func (d *StreamDecoder) IsInArray() bool
func (d *StreamDecoder) CurrentKey() string
func (d *StreamDecoder) CurrentIndex() int
func (d *StreamDecoder) Offset() int64

// StreamEncoder: Structural encoding (unchanged)
type StreamEncoder struct {
    // ...
}

func (e *StreamEncoder) BeginObject() error
func (e *StreamEncoder) EndObject() error
func (e *StreamEncoder) BeginArray() error
func (e *StreamEncoder) EndArray() error
func (e *StreamEncoder) WriteKey(key string) error
func (e *StreamEncoder) WriteString(value string) error
func (e *StreamEncoder) WriteInt(value int64) error
func (e *StreamEncoder) WriteFloat(value float64) error
func (e *StreamEncoder) WriteBool(value bool) error
func (e *StreamEncoder) WriteNull() error
```

## Conclusion

**Structural Event API**:
- ✅ Symmetric with encoder
- ✅ Simpler to use
- ✅ Hides low-level details
- ✅ Better for indexing

**Result**: Clean, symmetric API that matches the encoder's structural abstractions!
