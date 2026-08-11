# Hardest Single Internal Functionality Analysis

## Question

What's the hardest single internal functionality required in the `stream` package?

## Components Breakdown

### 1. State (stream/state.go)
**Complexity**: ⭐⭐ (Medium)

**What it does**:
- Track depth, path stack, name stack
- Update state on token processing
- Generate paths (e.g., "key", "key[0]")

**Why it's not hardest**:
- ✅ Straightforward state machine
- ✅ Similar patterns exist in codebase
- ✅ Well-understood problem (path tracking)

### 2. Encoder (stream/encoder.go)
**Complexity**: ⭐⭐⭐ (Medium-High)

**What it does**:
- Write structural events to bytes
- Format values (quote strings, format numbers)
- Track offsets
- Update state

**Why it's not hardest**:
- ✅ Can reuse `encode` package logic for formatting
- ✅ Writing is easier than reading (we control the flow)
- ✅ No buffering needed (just write)

**Challenges**:
- ⚠️ Formatting logic (but can reuse existing code)
- ⚠️ Offset tracking (but straightforward)

### 3. Decoder (stream/decoder.go)
**Complexity**: ⭐⭐⭐⭐⭐ (Hardest)

**What it does**:
- Read bytes from `io.Reader`
- Tokenize incrementally (handle partial tokens)
- Convert tokens to events
- Update state
- Distinguish keys from values

**Why it's hardest**:
- ❌ **Streaming tokenization** - Handle partial tokens, buffer management
- ❌ **Token-to-event conversion** - Complex state machine
- ❌ **Key vs value detection** - Requires context awareness
- ❌ **Nested structure handling** - Must track structure boundaries

### 4. Conversion (stream/conversion.go)
**Complexity**: ⭐⭐ (Medium)

**What it does**:
- `NodeToEvents`: Tree traversal, straightforward
- `EventsToNode`: Parse event sequence, straightforward

**Why it's not hardest**:
- ✅ Tree traversal is well-understood
- ✅ No I/O complexity
- ✅ No buffering needed

## The Hardest: Decoder's Token-to-Event Conversion

### Core Challenge: `ReadEvent()` Implementation

**What it needs to do**:

```go
func (d *Decoder) ReadEvent() (Event, error) {
    // 1. Read bytes from io.Reader (may be partial)
    // 2. Tokenize incrementally (handle partial tokens)
    // 3. Skip structural tokens (commas, colons)
    // 4. Convert tokens to events
    // 5. Determine if string is key or value
    // 6. Update internal state
    // 7. Return event
}
```

### Specific Hard Parts

#### 1. Streaming Tokenization with Buffering

**Problem**: `tokenizeOne()` can return `io.EOF` when more data is needed.

**What's needed**:
```go
type Decoder struct {
    reader io.Reader
    buffer []byte  // Unprocessed bytes
    // ...
}

func (d *Decoder) readTokenInternal() (token.Token, error) {
    for {
        // Try to tokenize from buffer
        tok, err := tokenizeOne(d.buffer, ...)
        if err != io.EOF {
            // Got a token, consume from buffer
            d.buffer = d.buffer[consumed:]
            return tok, nil
        }
        
        // Need more data - read from reader
        newData := make([]byte, 4096)
        n, err := d.reader.Read(newData)
        if err != nil && err != io.EOF {
            return token.Token{}, err
        }
        if n == 0 {
            return token.Token{}, io.EOF
        }
        
        // Append to buffer and retry
        d.buffer = append(d.buffer, newData[:n]...)
    }
}
```

**Challenges**:
- ⚠️ Buffer management (grow, shrink, handle partial tokens)
- ⚠️ Knowing how much to consume from buffer
- ⚠️ Handling `tokenizeOne()`'s return values correctly
- ⚠️ Edge cases (empty buffer, partial tokens at end)

#### 2. Token-to-Event Conversion

**Problem**: Convert low-level tokens to high-level events, skip structural tokens.

**What's needed**:
```go
func (d *Decoder) ReadEvent() (Event, error) {
    for {
        tok, err := d.readTokenInternal()
        if err != nil {
            return Event{}, err
        }
        
        // Skip structural tokens
        switch tok.Type {
        case token.TComma, token.TColon:
            continue  // Skip
        
        case token.TLCurl:
            d.state.ProcessToken(tok)
            return Event{Type: EventBeginObject}, nil
        
        case token.TRCurl:
            d.state.ProcessToken(tok)
            return Event{Type: EventEndObject}, nil
        
        case token.TString:
            d.state.ProcessToken(tok)
            // HARD PART: Is this a key or value?
            if d.state.IsInObject() && d.state.CurrentKey() == "" {
                // This is a key
                return Event{Type: EventKey, Key: tok.String}, nil
            } else {
                // This is a value
                return Event{Type: EventString, String: tok.String}, nil
            }
        
        // ... handle other token types
        }
    }
}
```

**Challenges**:
- ⚠️ **Key vs value detection** - Requires checking state before processing token
- ⚠️ **State synchronization** - Must update state at right time
- ⚠️ **Nested structures** - Must handle Begin/End correctly
- ⚠️ **Edge cases** - Empty objects/arrays, trailing commas, etc.

#### 3. State Synchronization

**Problem**: State must be updated correctly for key/value detection to work.

**Challenge**:
- State must be updated BEFORE checking if string is key or value
- But state update depends on token type
- Need to handle state transitions correctly

**Example**:
```go
// Reading: { "key": "value" }
tok := readToken()  // TLCurl
d.state.ProcessToken(tok)  // Now in object, depth=1

tok = readToken()  // TString("key")
// Need to check state BEFORE processing token
if d.state.IsInObject() && d.state.CurrentKey() == "" {
    // This is a key
    d.state.ProcessToken(tok)  // Update state with key
    return EventKey
}

tok = readToken()  // TColon - skip
tok = readToken()  // TString("value")
// State already has key, so this is a value
d.state.ProcessToken(tok)
return EventString
```

**Complexity**: State machine must be perfectly synchronized with token stream.

## Comparison with Existing Code

### token.Source (Similar Challenge)

Looking at `token/source.go`, it already does:
- ✅ Streaming tokenization with buffering
- ✅ Token reading with `tokenizeOne()`
- ✅ State management

**But**:
- ⚠️ Doesn't convert to events (returns tokens)
- ⚠️ Doesn't skip commas/colons
- ⚠️ Doesn't distinguish keys from values

**We can learn from it**, but need to add event conversion layer.

## The Hardest Single Functionality

### Winner: **Decoder's `ReadEvent()` Method**

**Why it's hardest**:

1. **Streaming Tokenization**:
   - Buffer management for partial tokens
   - Handling `tokenizeOne()`'s `io.EOF` return
   - Consuming correct amount from buffer
   - Edge cases (empty buffer, end of stream)

2. **Token-to-Event Conversion**:
   - Skip structural tokens (commas, colons)
   - Convert tokens to events
   - Handle all token types correctly

3. **Key vs Value Detection**:
   - Requires state awareness
   - Must check state at right time
   - Handle nested structures correctly

4. **State Synchronization**:
   - Update state at correct points
   - Maintain consistency
   - Handle edge cases

**Estimated Complexity**: ~300-400 lines of careful code

**Risk Areas**:
- ⚠️ Buffer management bugs (memory leaks, incorrect consumption)
- ⚠️ State synchronization bugs (wrong key/value detection)
- ⚠️ Edge case handling (empty structures, trailing commas, etc.)

## Mitigation Strategies

### 1. Reuse token.Source Logic

**Strategy**: Extract buffering logic from `token.Source`, adapt for events.

**Benefit**: 
- ✅ Proven code
- ✅ Already handles edge cases
- ✅ Reduces risk

### 2. Incremental Implementation

**Strategy**: 
1. First: Basic tokenization (no events)
2. Second: Add event conversion
3. Third: Add key/value detection
4. Fourth: Handle edge cases

**Benefit**:
- ✅ Test each layer independently
- ✅ Easier debugging
- ✅ Lower risk

### 3. Comprehensive Testing

**Strategy**: Test all edge cases:
- Empty objects/arrays
- Nested structures
- Partial tokens
- End of stream
- Invalid input

**Benefit**:
- ✅ Catch bugs early
- ✅ Confidence in correctness

## Conclusion

**Hardest Single Functionality**: **Decoder's `ReadEvent()` method**

**Reasons**:
1. Streaming tokenization with buffering
2. Token-to-event conversion
3. Key vs value detection
4. State synchronization

**Estimated Effort**: 3-4 days for careful implementation + testing

**Risk Level**: High (complex state machine, many edge cases)

**Mitigation**: Reuse `token.Source` patterns, incremental implementation, comprehensive testing
