# Stream ir.Node Conversion API

## The Idea

Add conversion functions in `stream` package:
- `NodeToEvents(node *ir.Node) ([]Event, error)` - Convert ir.Node to events
- `EventsToNode(events []Event) (*ir.Node, error)` - Convert events to ir.Node

## API Design

### stream Package Additions

```go
package stream

import "github.com/signadot/tony-format/go-tony/ir"

// NodeToEvents converts an ir.Node to a sequence of events.
// Returns events that can be written via StreamEncoder.
func NodeToEvents(node *ir.Node) ([]Event, error)

// EventsToNode converts a sequence of events to an ir.Node.
// Takes events read from StreamDecoder.
func EventsToNode(events []Event) (*ir.Node, error)

// EncodeNode encodes an ir.Node to bytes using StreamEncoder.
// Convenience function: NodeToEvents + StreamEncoder.
func EncodeNode(node *ir.Node, w io.Writer, opts ...StreamOption) error

// DecodeNode decodes bytes to ir.Node using StreamDecoder.
// Convenience function: StreamDecoder + EventsToNode.
func DecodeNode(r io.Reader, opts ...StreamOption) (*ir.Node, error)
```

## Implementation

### NodeToEvents

```go
func NodeToEvents(node *ir.Node) ([]Event, error) {
    var events []Event
    
    switch node.Type {
    case ir.ObjectType:
        events = append(events, Event{Type: EventBeginObject})
        
        for i, field := range node.Fields {
            // Add key event
            events = append(events, Event{
                Type: EventKey,
                Key:  field.String,
            })
            
            // Recursively convert value
            valueEvents, err := NodeToEvents(node.Values[i])
            if err != nil {
                return nil, err
            }
            events = append(events, valueEvents...)
        }
        
        events = append(events, Event{Type: EventEndObject})
        
    case ir.ArrayType:
        events = append(events, Event{Type: EventBeginArray})
        
        for _, value := range node.Values {
            valueEvents, err := NodeToEvents(value)
            if err != nil {
                return nil, err
            }
            events = append(events, valueEvents...)
        }
        
        events = append(events, Event{Type: EventEndArray})
        
    case ir.StringType:
        events = append(events, Event{
            Type:   EventString,
            String: node.String,
        })
        
    case ir.NumberType:
        if node.Int64 != nil {
            events = append(events, Event{
                Type: EventInt,
                Int:   *node.Int64,
            })
        } else if node.Float64 != nil {
            events = append(events, Event{
                Type:  EventFloat,
                Float: *node.Float64,
            })
        }
        
    case ir.BoolType:
        events = append(events, Event{
            Type: EventBool,
            Bool:  node.Bool,
        })
        
    case ir.NullType:
        events = append(events, Event{Type: EventNull})
        
    default:
        return nil, fmt.Errorf("unsupported node type: %v", node.Type)
    }
    
    return events, nil
}
```

### EventsToNode

```go
func EventsToNode(events []Event) (*ir.Node, error) {
    if len(events) == 0 {
        return nil, fmt.Errorf("empty events")
    }
    
    event := events[0]
    
    switch event.Type {
    case EventBeginObject:
        node := &ir.Node{
            Type:   ir.ObjectType,
            Fields: make([]*ir.Node, 0),
            Values: make([]*ir.Node, 0),
        }
        
        i := 1 // Skip BeginObject
        for i < len(events) {
            if events[i].Type == EventEndObject {
                break
            }
            
            // Expect key
            if events[i].Type != EventKey {
                return nil, fmt.Errorf("expected key, got %v", events[i].Type)
            }
            key := events[i].Key
            i++
            
            // Parse value
            value, consumed, err := parseValue(events[i:])
            if err != nil {
                return nil, err
            }
            
            node.Fields = append(node.Fields, ir.FromString(key))
            node.Values = append(node.Values, value)
            i += consumed
        }
        
        return node, nil
        
    case EventBeginArray:
        node := &ir.Node{
            Type:   ir.ArrayType,
            Values: make([]*ir.Node, 0),
        }
        
        i := 1 // Skip BeginArray
        for i < len(events) {
            if events[i].Type == EventEndArray {
                break
            }
            
            value, consumed, err := parseValue(events[i:])
            if err != nil {
                return nil, err
            }
            
            node.Values = append(node.Values, value)
            i += consumed
        }
        
        return node, nil
        
    case EventString:
        return ir.FromString(event.String), nil
        
    case EventInt:
        return ir.FromInt(event.Int), nil
        
    case EventFloat:
        return ir.FromFloat(event.Float), nil
        
    case EventBool:
        return ir.FromBool(event.Bool), nil
        
    case EventNull:
        return ir.Null(), nil
        
    default:
        return nil, fmt.Errorf("unexpected event type: %v", event.Type)
    }
}

// parseValue parses a value from events, returns node and number of events consumed
func parseValue(events []Event) (*ir.Node, int, error) {
    if len(events) == 0 {
        return nil, 0, fmt.Errorf("no events")
    }
    
    event := events[0]
    
    switch event.Type {
    case EventBeginObject, EventBeginArray:
        // Parse nested structure
        depth := 1
        i := 1
        for i < len(events) && depth > 0 {
            switch events[i].Type {
            case EventBeginObject, EventBeginArray:
                depth++
            case EventEndObject, EventEndArray:
                depth--
            }
            i++
        }
        node, err := EventsToNode(events[:i])
        return node, i, err
        
    case EventString, EventInt, EventFloat, EventBool, EventNull:
        node, err := EventsToNode(events[:1])
        return node, 1, err
        
    default:
        return nil, 0, fmt.Errorf("unexpected event type: %v", event.Type)
    }
}
```

### EncodeNode (Convenience)

```go
func EncodeNode(node *ir.Node, w io.Writer, opts ...StreamOption) error {
    events, err := NodeToEvents(node)
    if err != nil {
        return err
    }
    
    enc := NewStreamEncoder(w, opts...)
    return writeEvents(enc, events)
}

func writeEvents(enc *StreamEncoder, events []Event) error {
    for _, event := range events {
        switch event.Type {
        case EventBeginObject:
            if err := enc.BeginObject(); err != nil {
                return err
            }
        case EventEndObject:
            if err := enc.EndObject(); err != nil {
                return err
            }
        case EventBeginArray:
            if err := enc.BeginArray(); err != nil {
                return err
            }
        case EventEndArray:
            if err := enc.EndArray(); err != nil {
                return err
            }
        case EventKey:
            if err := enc.WriteKey(event.Key); err != nil {
                return err
            }
        case EventString:
            if err := enc.WriteString(event.String); err != nil {
                return err
            }
        case EventInt:
            if err := enc.WriteInt(event.Int); err != nil {
                return err
            }
        case EventFloat:
            if err := enc.WriteFloat(event.Float); err != nil {
                return err
            }
        case EventBool:
            if err := enc.WriteBool(event.Bool); err != nil {
                return err
            }
        case EventNull:
            if err := enc.WriteNull(); err != nil {
                return err
            }
        }
    }
    return enc.Flush()
}
```

### DecodeNode (Convenience)

```go
func DecodeNode(r io.Reader, opts ...StreamOption) (*ir.Node, error) {
    dec := NewStreamDecoder(r, opts...)
    
    var events []Event
    for {
        event, err := dec.ReadEvent()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        events = append(events, event)
    }
    
    return EventsToNode(events)
}
```

## How encode Package Would Use It

### Option 1: Use stream.EncodeNode Directly

```go
package encode

import "github.com/signadot/tony-format/go-tony/stream"

func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
    // Convert EncodeOption to StreamOption
    streamOpts := convertOptions(opts)
    
    // Use stream.EncodeNode
    return stream.EncodeNode(node, w, streamOpts...)
}
```

**Problem**: `stream.EncodeNode` doesn't handle all encoding options (format, colors, comments, block style, etc.)

### Option 2: Use NodeToEvents + Custom Encoding

```go
package encode

import "github.com/signadot/tony-format/go-tony/stream"

func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
    es := &EncState{indent: 2}
    for _, opt := range opts {
        opt(es)
    }
    
    // Convert to events
    events, err := stream.NodeToEvents(node)
    if err != nil {
        return err
    }
    
    // Write events with formatting
    return writeEventsWithFormatting(events, w, es)
}

func writeEventsWithFormatting(events []Event, w io.Writer, es *EncState) error {
    // Use StreamEncoder but apply formatting options
    // Or use existing encode logic but driven by events
    // ...
}
```

**Problem**: Still need to handle formatting, which might be easier with existing encode logic.

### Option 3: Keep Existing encode, Add stream Option

```go
package encode

import "github.com/signadot/tony-format/go-tony/stream"

func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
    // Check if streaming is requested
    if hasStreamOption(opts) {
        // Use stream.EncodeNode for simple cases
        return stream.EncodeNode(node, w)
    }
    
    // Otherwise use existing encode logic (handles formatting, etc.)
    return encode(node, w, es)
}
```

**Better**: Can use stream for simple cases, existing logic for complex formatting.

## How parse Package Would Use It

### Option 1: Use stream.DecodeNode Directly

```go
package parse

import "github.com/signadot/tony-format/go-tony/stream"

func Parse(data []byte) (*ir.Node, error) {
    return stream.DecodeNode(bytes.NewReader(data))
}
```

**Works**: Parsing doesn't have as many options as encoding.

### Option 2: Use StreamDecoder + EventsToNode

```go
package parse

import "github.com/signadot/tony-format/go-tony/stream"

func Parse(data []byte) (*ir.Node, error) {
    dec := stream.NewStreamDecoder(bytes.NewReader(data))
    
    var events []Event
    for {
        event, err := dec.ReadEvent()
        if err == io.EOF {
            break
        }
        if err != nil {
            return nil, err
        }
        events = append(events, event)
    }
    
    return stream.EventsToNode(events)
}
```

**Same as Option 1**: `DecodeNode` does this internally.

## Benefits of NodeToEvents/EventsToNode

### 1. Pure Conversion Functions

```go
// No I/O, just conversion
events, _ := stream.NodeToEvents(node)
node, _ := stream.EventsToNode(events)
```

**Benefits**:
- ✅ Testable without I/O
- ✅ Composable
- ✅ Can manipulate events before writing

### 2. Flexible Usage

```go
// Convert to events
events, _ := stream.NodeToEvents(node)

// Manipulate events
events = filterEvents(events)

// Write via encoder
enc := stream.NewStreamEncoder(writer)
for _, event := range events {
    writeEvent(enc, event)
}
```

### 3. Enables encode/parse to Use stream

```go
// encode package
func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
    events, _ := stream.NodeToEvents(node)
    // Apply formatting options
    return writeEventsWithFormatting(events, w, opts)
}

// parse package
func Parse(data []byte) (*ir.Node, error) {
    dec := stream.NewStreamDecoder(bytes.NewReader(data))
    events, _ := readAllEvents(dec)
    return stream.EventsToNode(events)
}
```

## Updated stream Package API

```go
package stream

import (
    "io"
    "github.com/signadot/tony-format/go-tony/ir"
)

// Core: Events ↔ Bytes
type StreamEncoder struct { ... }
type StreamDecoder struct { ... }

// Conversion: ir.Node ↔ Events
func NodeToEvents(node *ir.Node) ([]Event, error)
func EventsToNode(events []Event) (*ir.Node, error)

// Convenience: ir.Node ↔ Bytes
func EncodeNode(node *ir.Node, w io.Writer, opts ...StreamOption) error
func DecodeNode(r io.Reader, opts ...StreamOption) (*ir.Node, error)
```

## Summary

**stream Package**:
- ✅ `NodeToEvents()` - Pure conversion, no I/O
- ✅ `EventsToNode()` - Pure conversion, no I/O
- ✅ `EncodeNode()` - Convenience: NodeToEvents + StreamEncoder
- ✅ `DecodeNode()` - Convenience: StreamDecoder + EventsToNode

**encode Package**:
- Can use `NodeToEvents()` + apply formatting
- Or use `EncodeNode()` for simple cases

**parse Package**:
- Can use `DecodeNode()` directly
- Or use `StreamDecoder` + `EventsToNode()` for more control

**Result**: Clean separation, flexible usage, enables encode/parse to use stream internally!
