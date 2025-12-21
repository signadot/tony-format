# Stream Translation Responsibilities

## The Question

Who is responsible for translating:
1. `ir.Node` ↔ stream events
2. Bytes ↔ stream events (already handled by StreamEncoder/StreamDecoder)

## Current Architecture

### Current Flow

**Encoding** (`encode/encode.go`):
```
ir.Node → encode.Encode() → bytes
```

**Decoding** (`parse/parse.go`):
```
bytes → parse.Parse() → ir.Node
```

### New Flow with Stream Package

**Stream Package** (`stream/`):
```
bytes ↔ StreamEncoder/StreamDecoder ↔ events
```

**Question**: Where does `ir.Node` fit?

## Translation Layers

### Layer 1: Bytes ↔ Events (stream package)

**Responsibility**: `stream` package
- `StreamEncoder`: events → bytes
- `StreamDecoder`: bytes → events

**Already handled**: ✅ StreamEncoder/StreamDecoder do this

### Layer 2: ir.Node ↔ Events

**Question**: Who handles this?

## Options

### Option 1: stream Package Handles ir.Node Conversion

**API**:
```go
package stream

import "github.com/signadot/tony-format/go-tony/ir"

// EncodeNode encodes an ir.Node to bytes using StreamEncoder
func EncodeNode(node *ir.Node, w io.Writer) error {
    enc := NewStreamEncoder(w)
    return encodeNodeToEvents(node, enc)
}

// DecodeNode decodes bytes to ir.Node using StreamDecoder
func DecodeNode(r io.Reader) (*ir.Node, error) {
    dec := NewStreamDecoder(r)
    return decodeEventsToNode(dec)
}

// encodeNodeToEvents converts ir.Node to stream events
func encodeNodeToEvents(node *ir.Node, enc *StreamEncoder) error {
    switch node.Type {
    case ir.ObjectType:
        enc.BeginObject()
        for i, field := range node.Fields {
            enc.WriteKey(field.String)
            encodeNodeToEvents(node.Values[i], enc)
        }
        enc.EndObject()
    case ir.ArrayType:
        enc.BeginArray()
        for _, value := range node.Values {
            encodeNodeToEvents(value, enc)
        }
        enc.EndArray()
    case ir.StringType:
        enc.WriteString(node.String)
    case ir.NumberType:
        if node.Int64 != nil {
            enc.WriteInt(*node.Int64)
        } else {
            enc.WriteFloat(*node.Float64)
        }
    // ... etc
    }
}

// decodeEventsToNode converts stream events to ir.Node
func decodeEventsToNode(dec *StreamDecoder) (*ir.Node, error) {
    event, err := dec.ReadEvent()
    if err != nil {
        return nil, err
    }
    
    switch event.Type {
    case EventBeginObject:
        node := &ir.Node{Type: ir.ObjectType}
        for {
            event, err := dec.ReadEvent()
            if err == io.EOF || event.Type == EventEndObject {
                break
            }
            if event.Type == EventKey {
                node.Fields = append(node.Fields, ir.FromString(event.Key))
                value, err := decodeEventsToNode(dec)
                if err != nil {
                    return nil, err
                }
                node.Values = append(node.Values, value)
            }
        }
        return node, nil
    case EventString:
        return ir.FromString(event.String), nil
    case EventInt:
        return ir.FromInt(event.Int), nil
    // ... etc
    }
}
```

**Pros**:
- ✅ All stream-related functionality in one package
- ✅ Convenient for users who want ir.Node conversion

**Cons**:
- ⚠️ `stream` package depends on `ir` package
- ⚠️ Might not be needed for indexing use case (no ir.Node)

### Option 2: encode Package Uses StreamEncoder

**API**:
```go
package encode

import "github.com/signadot/tony-format/go-tony/stream"

// Encode encodes ir.Node to bytes using StreamEncoder internally
func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
    enc := stream.NewStreamEncoder(w)
    return encodeNodeToStream(node, enc, opts)
}

func encodeNodeToStream(node *ir.Node, enc *stream.StreamEncoder, opts []EncodeOption) error {
    // Convert ir.Node to stream events
    switch node.Type {
    case ir.ObjectType:
        enc.BeginObject()
        for i, field := range node.Fields {
            enc.WriteKey(field.String)
            encodeNodeToStream(node.Values[i], enc, opts)
        }
        enc.EndObject()
    // ... etc
    }
}
```

**Pros**:
- ✅ `encode` package already handles ir.Node → bytes
- ✅ Can use StreamEncoder as implementation detail
- ✅ `stream` package stays focused on events ↔ bytes

**Cons**:
- ⚠️ `encode` package depends on `stream` package
- ⚠️ Need to handle encoding options (format, colors, etc.)

### Option 3: parse Package Uses StreamDecoder

**API**:
```go
package parse

import "github.com/signadot/tony-format/go-tony/stream"

// Parse parses bytes to ir.Node using StreamDecoder internally
func Parse(data []byte) (*ir.Node, error) {
    dec := stream.NewStreamDecoder(bytes.NewReader(data))
    return parseStreamToNode(dec)
}

func parseStreamToNode(dec *stream.StreamDecoder) (*ir.Node, error) {
    // Convert stream events to ir.Node
    event, err := dec.ReadEvent()
    if err != nil {
        return nil, err
    }
    
    switch event.Type {
    case stream.EventBeginObject:
        // ... build ir.Node from events
    // ... etc
    }
}
```

**Pros**:
- ✅ `parse` package already handles bytes → ir.Node
- ✅ Can use StreamDecoder as implementation detail
- ✅ `stream` package stays focused on events ↔ bytes

**Cons**:
- ⚠️ `parse` package depends on `stream` package

### Option 4: Separate Conversion Package

**API**:
```go
package streamconv  // or stream/conv

import (
    "github.com/signadot/tony-format/go-tony/ir"
    "github.com/signadot/tony-format/go-tony/stream"
)

// NodeToEvents converts ir.Node to stream events
func NodeToEvents(node *ir.Node, enc *stream.StreamEncoder) error

// EventsToNode converts stream events to ir.Node
func EventsToNode(dec *stream.StreamDecoder) (*ir.Node, error)
```

**Pros**:
- ✅ Clear separation of concerns
- ✅ `stream` package doesn't depend on `ir`
- ✅ Optional - only needed if you want ir.Node conversion

**Cons**:
- ⚠️ Another package to maintain
- ⚠️ Might be overkill

## Analysis: Use Cases

### Use Case 1: Indexing (No ir.Node)

```go
// Read existing chunk
dec := stream.NewStreamDecoder(chunkReader)

// Write new chunk
enc := stream.NewStreamEncoder(newSnapshotWriter)

// Process events directly
for {
    event, _ := dec.ReadEvent()
    // Process event, write to encoder
    switch event.Type {
    case stream.EventBeginArray:
        enc.BeginArray()
    // ... etc
    }
}
```

**Need**: ✅ Events ↔ bytes (already handled)
**Need**: ❌ ir.Node conversion (not needed)

### Use Case 2: Parsing (ir.Node needed)

```go
// Parse bytes to ir.Node
node, err := parse.Parse(data)
```

**Need**: ✅ Bytes → ir.Node
**Option**: `parse` package uses `StreamDecoder` internally

### Use Case 3: Encoding (ir.Node needed)

```go
// Encode ir.Node to bytes
err := encode.Encode(node, writer)
```

**Need**: ✅ ir.Node → bytes
**Option**: `encode` package uses `StreamEncoder` internally

## Recommendation

### Primary: stream Package = Events ↔ Bytes Only

**`stream` package responsibility**:
- ✅ Events ↔ bytes (StreamEncoder/StreamDecoder)
- ❌ NOT ir.Node conversion

**Rationale**:
- Keeps `stream` package focused
- No dependency on `ir` package
- Indexing use case doesn't need ir.Node

### Secondary: encode/parse Packages Use stream Internally

**`encode` package**:
- Uses `StreamEncoder` internally for `Encode()`
- Converts `ir.Node` → events → bytes
- Handles encoding options (format, colors, etc.)

**`parse` package**:
- Uses `StreamDecoder` internally for `Parse()`
- Converts bytes → events → `ir.Node`
- Handles parsing options

**Benefits**:
- ✅ `stream` package stays focused
- ✅ Existing APIs (`encode.Encode`, `parse.Parse`) continue to work
- ✅ Can use stream internally for better performance/features
- ✅ Indexing can use stream directly (no ir.Node overhead)

## Implementation Strategy

### Phase 1: stream Package (Events ↔ Bytes)

```go
package stream

// StreamEncoder: events → bytes
// StreamDecoder: bytes → events
// No ir.Node dependency
```

### Phase 2: encode Package Uses StreamEncoder

```go
package encode

import "github.com/signadot/tony-format/go-tony/stream"

func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
    enc := stream.NewStreamEncoder(w)
    // Apply options (format, colors, etc.)
    // Convert ir.Node to events
    return encodeNodeToStream(node, enc, opts)
}
```

### Phase 3: parse Package Uses StreamDecoder

```go
package parse

import "github.com/signadot/tony-format/go-tony/stream"

func Parse(data []byte) (*ir.Node, error) {
    dec := stream.NewStreamDecoder(bytes.NewReader(data))
    return parseStreamToNode(dec)
}
```

## Summary

**stream package**:
- ✅ Events ↔ bytes only
- ❌ No ir.Node conversion
- ❌ No dependency on `ir` package

**encode package**:
- ✅ Uses `StreamEncoder` internally
- ✅ Converts `ir.Node` → events → bytes
- ✅ Handles encoding options

**parse package**:
- ✅ Uses `StreamDecoder` internally
- ✅ Converts bytes → events → `ir.Node`
- ✅ Handles parsing options

**Indexing**:
- ✅ Uses `stream` package directly
- ✅ No ir.Node conversion needed
- ✅ Events ↔ bytes only

**Result**: Clear separation of concerns, `stream` package stays focused!
