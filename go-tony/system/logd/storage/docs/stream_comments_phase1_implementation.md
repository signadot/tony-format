# Stream Comments: Phase 1 Implementation

## Design: Comment-Ready API

Make the API comment-ready without adding implementation complexity in Phase 1.

## API Additions

### Event Types

```go
type EventType int

const (
    EventBeginObject EventType = iota
    EventEndObject
    EventBeginArray
    EventEndArray
    EventKey
    EventString
    EventInt
    EventFloat
    EventBool
    EventNull
    EventHeadComment  // Head comment (precedes a value)
    EventLineComment  // Line comment (on same line as value)
)
```

### Event Structure

```go
type Event struct {
    Type EventType
    
    // Value fields
    Key      string
    String   string
    Int      int64
    Float    float64
    Bool     bool
    
    // Comment fields (for EventHeadComment and EventLineComment)
    CommentLines []string  // Comment text lines (from IR Node.Lines)
}
```

### Encoder Methods

```go
// Write head comment (precedes a value)
// IR: CommentType node with 1 value in Values
func (e *Encoder) WriteHeadComment(lines []string) error {
    // Phase 1: No-op
    // Phase 2: Write comment tokens before next value
    return nil
}

// Write line comment (on same line as value)
// IR: CommentType node in Comment field
func (e *Encoder) WriteLineComment(lines []string) error {
    // Phase 1: No-op
    // Phase 2: Write comment tokens after current value
    return nil
}
```

### Decoder Implementation

```go
func (d *Decoder) ReadEvent() (Event, error) {
    for {
        tok, err := d.readTokenInternal()
        if err != nil {
            return Event{}, err
        }
        
        // Phase 1: Skip comment tokens
        if tok.Type == token.TComment {
            continue  // Skip in Phase 1
        }
        
        // Convert other tokens to events...
        switch tok.Type {
        case token.TLCurl:
            d.state.ProcessToken(tok)
            return Event{Type: EventBeginObject}, nil
        
        case token.TString:
            d.state.ProcessToken(tok)
            // ... key vs value detection ...
        
        // ... other token types
        }
    }
}
```

**Phase 2 Implementation** (when needed):
```go
// Handle comment tokens
if tok.Type == token.TComment {
    // Determine if head or line comment based on context
    // Check position relative to last token/value
    isLineComment := isOnSameLineAsValue(tok, d.lastValueToken)
    
    if isLineComment {
        return Event{
            Type: EventLineComment,
            CommentLines: []string{string(tok.Bytes)},
        }, nil
    } else {
        return Event{
            Type: EventHeadComment,
            CommentLines: []string{string(tok.Bytes)},
        }, nil
    }
}
```

## Alignment with IR Specification

### Head Comments

**IR Model**:
```go
// CommentType node with 1 value
commentNode := &Node{
    Type: CommentType,
    Lines: []string{"# Head comment"},
    Values: []*Node{targetNode},  // Associated node
}
```

**Stream API**:
```go
// Encoding
enc.WriteHeadComment([]string{"# Head comment"})
enc.WriteKey("name")

// Decoding (Phase 2)
event, _ := dec.ReadEvent()  // EventHeadComment
event, _ := dec.ReadEvent()  // EventKey("name")
```

**Conversion (Phase 2)**:
```go
// EventsToNode
if event.Type == EventHeadComment {
    commentNode := &Node{
        Type: CommentType,
        Lines: event.CommentLines,
    }
    // Next event is the associated value
    valueNode, _ := EventsToNode(remainingEvents)
    commentNode.Values = []*Node{valueNode}
    return commentNode
}
```

### Line Comments

**IR Model**:
```go
// CommentType node in Comment field
targetNode := &Node{
    Type: StringType,
    String: "value",
    Comment: &Node{
        Type: CommentType,
        Lines: []string{"# Line comment"},
        Values: nil,  // Empty - it's a line comment
    },
}
```

**Stream API**:
```go
// Encoding
enc.WriteString("value")
enc.WriteLineComment([]string{"# Line comment"})

// Decoding (Phase 2)
event, _ := dec.ReadEvent()  // EventString("value")
event, _ := dec.ReadEvent()  // EventLineComment
```

**Conversion (Phase 2)**:
```go
// EventsToNode
valueNode := &Node{Type: StringType, String: event.String}
nextEvent, _ := dec.ReadEvent()
if nextEvent.Type == EventLineComment {
    valueNode.Comment = &Node{
        Type: CommentType,
        Lines: nextEvent.CommentLines,
    }
}
return valueNode
```

## Phase 1 Implementation Complexity

**What's Added**:
- ✅ Two event types (`EventHeadComment`, `EventLineComment`)
- ✅ `CommentLines` field in `Event`
- ✅ Two no-op methods in `Encoder`
- ✅ Skip comment tokens in `Decoder.ReadEvent()`

**Code Added**: ~20-30 lines

**Complexity**: ⭐ (Minimal)

**Benefits**:
- ✅ API is comment-ready
- ✅ Aligned with IR specification
- ✅ Easy to extend later
- ✅ No implementation complexity in Phase 1

## Phase 2 Implementation (Future)

**What's Needed**:
- Parse `TComment` tokens
- Determine head vs line comment (context-dependent)
- Emit comment events
- Write comments in encoder
- Handle comment associations in conversion

**Code Added**: ~100-150 lines

**Complexity**: ⭐⭐⭐ (Medium)

## Summary

**Phase 1**: Comment-ready API with no-op implementation
- ✅ Add event types and fields
- ✅ Add no-op methods
- ✅ Skip comment tokens
- ✅ ~20-30 lines, minimal complexity

**Phase 2**: Full comment support (if needed)
- Parse and emit comment events
- Write comments
- Handle associations
- ~100-150 lines, medium complexity

**Result**: API designed for comments, implementation deferred until needed.
