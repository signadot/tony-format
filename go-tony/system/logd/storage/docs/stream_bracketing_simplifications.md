# Bracketing Assumption Simplifications

## Question

What simplifications are available for streaming tokenization with buffering given the bracketing assumption?

## Bracketing Assumption

**Assumption**: Only bracketed structures (`{...}` and `[...]`) are supported.

**What this means**:
- ✅ No block style (indentation-based arrays)
- ✅ No `TArrayElt` tokens
- ✅ Explicit structure boundaries (brackets)
- ✅ No indentation tracking for structure boundaries

## Simplifications Available

### 1. No Block Style Handling

**Without Bracketing** (Complex):
```go
// Must handle both:
[1, 2, 3]           // Bracketed array
- 1                 // Block style array
- 2
- 3
```

**With Bracketing** (Simple):
```go
// Only handle:
[1, 2, 3]           // Bracketed array only
```

**Simplifications**:
- ✅ **No `TArrayElt` tokens** - Don't need to handle block-style array elements
- ✅ **No indentation tracking** - Don't need to track indentation for structure boundaries
- ✅ **Simpler state machine** - Only bracket-based state transitions
- ✅ **Easier buffering** - No need to handle partial indentation

**Code Reduction**: ~50-100 lines

### 2. Simpler Buffer Management

**Without Bracketing** (Complex):
```go
// Must handle partial indentation
buffer := "  - "  // Partial token? Or complete?
// Need to check if next line continues indentation
```

**With Bracketing** (Simple):
```go
// Brackets are single characters - always complete
buffer := "{"  // Complete token, no ambiguity
buffer := "["  // Complete token, no ambiguity
```

**Simplifications**:
- ✅ **No partial indentation** - Brackets are single bytes
- ✅ **Clear boundaries** - `{` and `}` are unambiguous
- ✅ **Easier token detection** - Can peek ahead safely
- ✅ **Simpler buffer growth** - Don't need to preserve indentation context

**Code Reduction**: ~30-50 lines

### 3. Simpler State Machine

**Without Bracketing** (Complex):
```go
type state int
const (
    stateObject state = iota
    stateArray
    stateBlockArray  // Block style array
    stateIndent      // Tracking indentation
    // ...
)
```

**With Bracketing** (Simple):
```go
type state int
const (
    stateObject state = iota
    stateArray
    // That's it!
)
```

**Simplifications**:
- ✅ **Fewer states** - Only object and array
- ✅ **No indentation state** - Don't track indentation levels
- ✅ **Simpler transitions** - Only bracket-based transitions
- ✅ **Easier to reason about** - Clear state machine

**Code Reduction**: ~50-100 lines

### 4. Simpler Path Tracking

**Without Bracketing** (Complex):
```go
// Must handle block-style paths
"array[0]"          // Bracketed
"array"             // Block style - which element?
// Need to track indentation to determine element index
```

**With Bracketing** (Simple):
```go
// Only bracketed paths
"array[0]"          // Clear index
"object.key"        // Clear key
```

**Simplifications**:
- ✅ **No block-style path ambiguity** - All paths are explicit
- ✅ **Easier index tracking** - Array indices are explicit
- ✅ **Simpler path generation** - No indentation-based logic

**Code Reduction**: ~30-50 lines

### 5. Simpler Token-to-Event Conversion

**Without Bracketing** (Complex):
```go
switch tok.Type {
case token.TArrayElt:
    // Block style element - need to handle specially
    // Track indentation, determine if new element
case token.TIndent:
    // Might be structure boundary, might be formatting
    // Need context to determine
}
```

**With Bracketing** (Simple):
```go
switch tok.Type {
case token.TLCurl:
    return EventBeginObject
case token.TRCurl:
    return EventEndObject
case token.TLSquare:
    return EventBeginArray
case token.TRSquare:
    return EventEndArray
// No TArrayElt, simpler TIndent handling
}
```

**Simplifications**:
- ✅ **No `TArrayElt` handling** - Can skip entirely
- ✅ **Simpler `TIndent` handling** - Just formatting, not structure
- ✅ **Fewer token types** - Don't need to handle block-style tokens
- ✅ **Clearer conversion** - Direct mapping from tokens to events

**Code Reduction**: ~50-100 lines

### 6. Simpler Error Handling

**Without Bracketing** (Complex):
```go
// Must handle:
- Unmatched indentation
- Block style errors
- Mixed block/bracketed style
```

**With Bracketing** (Simple):
```go
// Only handle:
- Unmatched brackets
- Invalid bracket nesting
```

**Simplifications**:
- ✅ **Fewer error cases** - Only bracket-related errors
- ✅ **Clearer errors** - Bracket mismatch is obvious
- ✅ **Easier validation** - Can validate brackets easily

**Code Reduction**: ~20-30 lines

## Total Simplification Estimate

**Code Reduction**: ~230-380 lines of complexity removed

**Complexity Reduction**:
- ✅ Simpler buffering logic
- ✅ Simpler state machine
- ✅ Simpler path tracking
- ✅ Simpler token-to-event conversion
- ✅ Fewer edge cases

## Buffering Simplifications Specifically

### Before (With Block Style)

```go
func (d *Decoder) readTokenInternal() (token.Token, error) {
    for {
        // Try to tokenize
        tok, consumed, err := tokenizeOne(d.buffer, d.pos, ...)
        
        if err == io.EOF {
            // Might be:
            // 1. Need more data (simple case)
            // 2. Partial indentation (complex case)
            // 3. Partial block element (complex case)
            
            // Must check if we have partial indentation
            if hasPartialIndent(d.buffer, d.pos) {
                // Need to preserve indentation context
                // Read more, but be careful about indentation
            }
            
            // Read more data
            newData := readMore()
            d.buffer = append(d.buffer, newData...)
            continue
        }
        
        // Consume token
        d.buffer = d.buffer[consumed:]
        return tok, nil
    }
}
```

### After (Bracketed Only)

```go
func (d *Decoder) readTokenInternal() (token.Token, error) {
    for {
        // Try to tokenize
        tok, consumed, err := tokenizeOne(d.buffer, d.pos, ...)
        
        if err == io.EOF {
            // Simple: Just need more data
            // No partial indentation to worry about
            // No block elements to preserve
            
            newData := readMore()
            if len(newData) == 0 {
                return token.Token{}, io.EOF
            }
            d.buffer = append(d.buffer, newData...)
            continue
        }
        
        // Consume token
        d.buffer = d.buffer[consumed:]
        return tok, nil
    }
}
```

**Key Simplifications**:
- ✅ **No partial indentation checks** - Brackets are single bytes
- ✅ **No block element preservation** - Don't need to preserve block context
- ✅ **Simpler buffer growth** - Just append, no special handling
- ✅ **Easier EOF detection** - Clear when we're done

## Comments: Missing Feature

### Current API (No Comments)

```go
// Encoder
enc.BeginObject()
enc.WriteKey("name")
enc.WriteString("value")
enc.EndObject()
// ❌ No way to write comments

// Decoder
event, _ := dec.ReadEvent()  // EventBeginObject
event, _ := dec.ReadEvent()  // EventKey("name")
// ❌ Comments are skipped/ignored
```

### Should We Support Comments?

**Arguments FOR**:
- ✅ Comments are part of Tony format
- ✅ For completeness, API should support them
- ✅ Some use cases might need to preserve comments

**Arguments AGAINST**:
- ⚠️ Indexing use case doesn't need comments
- ⚠️ Adds complexity to API
- ⚠️ Comments are formatting, not structure

### Options

#### Option 1: Skip Comments (Current)

**API**: Comments are silently skipped

**Pros**:
- ✅ Simple API
- ✅ Focused on structure

**Cons**:
- ❌ Loses comment information
- ❌ Not complete Tony support

#### Option 2: Add Comment Events

**API**:
```go
type EventType int
const (
    // ... existing events
    EventComment  // Comment event
)

type Event struct {
    Type EventType
    Comment string  // Comment text
    // ...
}

// Encoder
func (e *Encoder) WriteComment(text string) error
```

**Usage**:
```go
// Decoding
event, _ := dec.ReadEvent()  // EventComment("# comment")
event, _ := dec.ReadEvent()  // EventBeginObject

// Encoding
enc.WriteComment("# comment")
enc.BeginObject()
```

**Pros**:
- ✅ Complete Tony support
- ✅ Can preserve comments

**Cons**:
- ⚠️ Adds API complexity
- ⚠️ Indexing doesn't need it

#### Option 3: Optional Comment Support

**API**:
```go
type StreamOption func(*streamOpts)

func WithComments() StreamOption {
    return func(opts *streamOpts) {
        opts.comments = true
    }
}

// If comments enabled, emit EventComment
// If disabled, skip comments
```

**Pros**:
- ✅ Flexible - can enable if needed
- ✅ Simple by default

**Cons**:
- ⚠️ Still adds complexity

### Recommendation

**For Phase 1**: **Skip Comments**

**Rationale**:
1. ✅ Indexing use case doesn't need comments
2. ✅ Keeps API simple
3. ✅ Can add later if needed
4. ✅ Comments are formatting, not structure

**For Future**: Can add comment support if needed:
- Add `EventComment` type
- Add `WriteComment()` to Encoder
- Emit `EventComment` in Decoder when comments enabled

## Summary

### Bracketing Simplifications

**Total**: ~230-380 lines of complexity removed

**Key Simplifications**:
1. ✅ No block style (`TArrayElt`) - ~50-100 lines
2. ✅ Simpler buffering - ~30-50 lines
3. ✅ Simpler state machine - ~50-100 lines
4. ✅ Simpler path tracking - ~30-50 lines
5. ✅ Simpler token conversion - ~50-100 lines
6. ✅ Simpler error handling - ~20-30 lines

**Result**: Much simpler streaming tokenization!

### Comments

**Recommendation**: Skip for Phase 1, add later if needed

**Rationale**: Indexing doesn't need comments, keeps API simple
