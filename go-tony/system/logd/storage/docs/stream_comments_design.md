# Stream Comments Design

## IR Comment Specification

From `ir/doc.go`:

### Comment Association

**CommentType nodes** define comment association. Comment content is placed in `Lines` (array of strings).

**A comment node either**:
1. **Head comment**: Contains 1 element in `Values`, a non-comment node to which it is associated
2. **Line comment**: Contains 0 elements and resides in the `Comment` field of a non-comment node, representing its line comment plus possibly trailing comment material

**A comment node may not represent both** a head comment and a line comment.

### Comment Structure

```go
type Node struct {
    Type    Type
    Comment *Node  // Line comment (if present)
    Values  []*Node
    // ...
}

// Comment node structure:
// - Type: CommentType
// - Lines: []string  // Comment text lines
// - Values: []*Node  // If head comment, contains 1 element (the associated node)
//                     // If line comment, empty (node is in parent's Comment field)
```

### Example

```go
// Head comment
commentNode := &Node{
    Type: CommentType,
    Lines: []string{"# This is a head comment"},
    Values: []*Node{targetNode},  // Associated node
}

// Line comment
targetNode := &Node{
    Type: StringType,
    String: "value",
    Comment: &Node{  // Line comment
        Type: CommentType,
        Lines: []string{"# This is a line comment"},
        Values: nil,  // Empty - it's a line comment
    },
}
```

## Stream API Design for Comments

### Design Principle

**Make it comment-ready** without adding much complexity. The API should:
1. ✅ Support comment events (even if we skip them in Phase 1)
2. ✅ Align with IR comment associations
3. ✅ Not complicate the core implementation
4. ✅ Be optional (can skip comments if not needed)

### Option 1: Comment Events (Recommended)

**Add comment events to the API**:

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

type Event struct {
    Type EventType
    
    // Value fields
    Key      string
    String   string
    Int      int64
    Float    float64
    Bool     bool
    
    // Comment fields
    CommentLines []string  // Comment text lines
}
```

### Encoder API

```go
// Write head comment (precedes a value)
func (e *Encoder) WriteHeadComment(lines []string) error

// Write line comment (on same line as value)
func (e *Encoder) WriteLineComment(lines []string) error
```

**Usage**:
```go
enc.BeginObject()

// Head comment (precedes key)
enc.WriteHeadComment([]string{"# Head comment"})
enc.WriteKey("name")

// Line comment (on same line as value)
enc.WriteString("value")
enc.WriteLineComment([]string{"# Line comment"})

enc.EndObject()
```

### Decoder API

**Events are emitted**:
```go
dec, _ := stream.NewDecoder(reader, stream.WithBrackets())

event, _ := dec.ReadEvent()  // EventBeginObject
event, _ := dec.ReadEvent()  // EventHeadComment (if present)
event, _ := dec.ReadEvent()  // EventKey("name")
event, _ := dec.ReadEvent()  // EventString("value")
event, _ := dec.ReadEvent()  // EventLineComment (if present)
event, _ := dec.ReadEvent()  // EventEndObject
```

### Implementation Strategy

#### Phase 1: Comment-Ready API (Skip Comments)

**API Design**:
- ✅ Add `EventHeadComment` and `EventLineComment` types
- ✅ Add `WriteHeadComment()` and `WriteLineComment()` methods
- ✅ Add `CommentLines` field to `Event`
- ⚠️ **But**: Skip comment tokens in `ReadEvent()` (return `nil` events or skip)
- ⚠️ **But**: `WriteHeadComment()`/`WriteLineComment()` are no-ops

**Rationale**:
- ✅ API is comment-ready
- ✅ Can add implementation later
- ✅ No complexity in Phase 1
- ✅ Callers can ignore comment events

#### Phase 2: Add Comment Support (If Needed)

**Implementation**:
- Parse comment tokens (`TComment`)
- Emit comment events
- Handle comment associations (head vs line)
- Write comments in encoder

### Option 2: Comment-Aware State

**Track comment context**:

```go
type Decoder struct {
    // ...
    pendingHeadComment []string  // Head comment waiting for next value
    currentLineComment []string  // Line comment for current value
}

func (d *Decoder) ReadEvent() (Event, error) {
    // Read comment tokens
    // Track as head comment or line comment
    // Emit comment events at right time
}
```

**Complexity**: Medium - need to track comment context

### Option 3: Comments in Events (Attached)

**Attach comments to value events**:

```go
type Event struct {
    Type EventType
    
    // Value fields
    String string
    
    // Comment fields (optional)
    HeadComment []string  // Head comment preceding this value
    LineComment []string  // Line comment on same line
}
```

**Usage**:
```go
event, _ := dec.ReadEvent()  // EventKey("name")
// event.HeadComment = []string{"# comment"}  // If present
// event.LineComment = nil

event, _ := dec.ReadEvent()  // EventString("value")
// event.HeadComment = nil
// event.LineComment = []string{"# comment"}  // If present
```

**Pros**:
- ✅ Comments attached to values (matches IR model)
- ✅ Simpler event stream (fewer events)

**Cons**:
- ⚠️ More complex event structure
- ⚠️ Need to buffer comments until value is read

### Recommendation: Option 1 (Comment Events)

**Why**:
1. ✅ **Simple**: Just add event types
2. ✅ **Flexible**: Can skip comments if not needed
3. ✅ **Aligned**: Matches IR model (head vs line comments)
4. ✅ **Extensible**: Easy to add implementation later

**Phase 1 Implementation**:
```go
// Event types (add to existing)
const (
    // ... existing events
    EventHeadComment
    EventLineComment
)

// Event struct (add field)
type Event struct {
    Type EventType
    // ... existing fields
    CommentLines []string  // For comment events
}

// Encoder methods (no-ops in Phase 1)
func (e *Encoder) WriteHeadComment(lines []string) error {
    // No-op for Phase 1
    return nil
}

func (e *Encoder) WriteLineComment(lines []string) error {
    // No-op for Phase 1
    return nil
}

// Decoder (skip comments in Phase 1)
func (d *Decoder) ReadEvent() (Event, error) {
    for {
        tok, err := d.readTokenInternal()
        if err != nil {
            return Event{}, err
        }
        
        // Skip comment tokens in Phase 1
        if tok.Type == token.TComment {
            continue
        }
        
        // ... rest of conversion
    }
}
```

**Phase 2 Implementation** (if needed):
```go
// Decoder (emit comment events)
func (d *Decoder) ReadEvent() (Event, error) {
    for {
        tok, err := d.readTokenInternal()
        if err != nil {
            return Event{}, err
        }
        
        // Handle comment tokens
        if tok.Type == token.TComment {
            // Determine if head or line comment
            // Emit appropriate event
            return Event{
                Type: EventHeadComment,  // or EventLineComment
                CommentLines: []string{string(tok.Bytes)},
            }, nil
        }
        
        // ... rest of conversion
    }
}
```

## Alignment with IR Specification

### Head Comments

**IR Model**:
```go
commentNode := &Node{
    Type: CommentType,
    Lines: []string{"# comment"},
    Values: []*Node{targetNode},
}
```

**Stream API**:
```go
// Encoding
enc.WriteHeadComment([]string{"# comment"})
enc.WriteKey("name")

// Decoding
event, _ := dec.ReadEvent()  // EventHeadComment
event, _ := dec.ReadEvent()  // EventKey("name")
```

**Conversion**:
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
targetNode := &Node{
    Type: StringType,
    String: "value",
    Comment: &Node{
        Type: CommentType,
        Lines: []string{"# comment"},
    },
}
```

**Stream API**:
```go
// Encoding
enc.WriteString("value")
enc.WriteLineComment([]string{"# comment"})

// Decoding
event, _ := dec.ReadEvent()  // EventString("value")
event, _ := dec.ReadEvent()  // EventLineComment
```

**Conversion**:
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

## Implementation Complexity

### Phase 1 (Comment-Ready, Skip Comments)

**Complexity**: ⭐ (Minimal)

**What's needed**:
- Add `EventHeadComment` and `EventLineComment` constants
- Add `CommentLines []string` field to `Event`
- Add no-op methods to `Encoder`
- Skip `TComment` tokens in `Decoder.ReadEvent()`

**Code**: ~20-30 lines

### Phase 2 (Full Comment Support)

**Complexity**: ⭐⭐⭐ (Medium)

**What's needed**:
- Parse `TComment` tokens
- Determine head vs line comment (context-dependent)
- Emit comment events
- Write comments in encoder
- Handle comment associations in conversion

**Code**: ~100-150 lines

## Summary

### Recommendation

**Phase 1**: **Comment-Ready API (Skip Comments)**

**API Design**:
- ✅ Add `EventHeadComment` and `EventLineComment` types
- ✅ Add `CommentLines` field to `Event`
- ✅ Add `WriteHeadComment()` and `WriteLineComment()` methods (no-ops)
- ✅ Skip comment tokens in `ReadEvent()` (for now)

**Benefits**:
- ✅ API is comment-ready
- ✅ Aligned with IR specification
- ✅ Minimal complexity (~20-30 lines)
- ✅ Easy to extend later

**Phase 2**: **Add Comment Support** (if needed)

- Parse comment tokens
- Emit comment events
- Write comments
- Handle associations

**Result**: API is designed for comments, but implementation is deferred until needed.
