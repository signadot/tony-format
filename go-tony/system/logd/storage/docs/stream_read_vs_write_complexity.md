# Stream Read vs Write Complexity Analysis

## Question

Is the hardest functionality on the **read side** (Decoder) or **write side** (Encoder)?

## Write Side: Encoder

### What Encoder Does

```go
enc, _ := stream.NewEncoder(writer, stream.WithBrackets())
enc.BeginObject()
enc.WriteKey("name")
enc.WriteString("value")
enc.EndObject()
```

### Complexity Analysis

**What's Required**:
1. ✅ Format values (quote strings, format numbers)
2. ✅ Write bytes to `io.Writer`
3. ✅ Track offsets
4. ✅ Update state (depth, path, etc.)

**Why It's Easier**:
- ✅ **Deterministic**: We control what we write
- ✅ **No buffering**: Just write directly
- ✅ **Reuse existing code**: Can use `encode` package logic for formatting
- ✅ **Simple state**: We know what structure we're in
- ✅ **No ambiguity**: We know if we're writing a key or value

**Challenges**:
- ⚠️ Formatting logic (but can reuse `encode` package)
- ⚠️ Offset tracking (but straightforward - just count bytes written)

**Estimated Complexity**: ⭐⭐⭐ (Medium)
- ~200-300 lines
- Mostly straightforward
- Can reuse existing formatting code

## Read Side: Decoder

### What Decoder Does

```go
dec, _ := stream.NewDecoder(reader, stream.WithBrackets())
event, _ := dec.ReadEvent()  // EventBeginObject
event, _ := dec.ReadEvent()  // EventKey("name")
event, _ := dec.ReadEvent()  // EventString("value")
event, _ := dec.ReadEvent()  // EventEndObject
```

### Complexity Analysis

**What's Required**:
1. ❌ **Read bytes from `io.Reader`** (non-deterministic)
2. ❌ **Tokenize incrementally** (handle partial tokens)
3. ❌ **Buffer management** (grow, shrink, track position)
4. ❌ **Convert tokens to events** (skip commas/colons)
5. ❌ **Detect keys vs values** (requires state awareness)
6. ❌ **Update state correctly** (synchronization)

**Why It's Harder**:
- ❌ **Non-deterministic**: Don't know what's coming
- ❌ **Buffering complexity**: Must handle partial tokens
- ❌ **State synchronization**: Must update state at right time for key/value detection
- ❌ **Edge cases**: Empty structures, trailing commas, partial tokens at EOF
- ❌ **Context awareness**: Need to know structure context to detect keys

**Specific Hard Parts**:

#### 1. Streaming Tokenization
```go
// tokenizeOne() can return io.EOF when more data needed
// Must buffer and retry
for {
    tok, err := tokenizeOne(buffer, pos, ...)
    if err == io.EOF {
        // Read more data, append to buffer, retry
        newData := readMore()
        buffer = append(buffer, newData...)
        continue
    }
    // Got token, consume from buffer
    buffer = buffer[consumed:]
    break
}
```

**Challenges**:
- Buffer growth/shrinking
- Tracking position correctly
- Handling partial tokens
- EOF detection

#### 2. Key vs Value Detection
```go
// Reading: { "key": "value" }
tok := readToken()  // TLCurl
state.ProcessToken(tok)  // Now in object

tok = readToken()  // TString("key")
// HARD: Is this a key or value?
// Must check state BEFORE processing token
if state.IsInObject() && state.CurrentKey() == "" {
    // This is a key
    state.ProcessToken(tok)  // Update state with key
    return EventKey
}

tok = readToken()  // TColon - skip
tok = readToken()  // TString("value")
// State already has key, so this is a value
state.ProcessToken(tok)
return EventString
```

**Challenges**:
- Must check state at right time
- State must be synchronized correctly
- Handle nested structures
- Edge cases (empty objects, etc.)

#### 3. Token-to-Event Conversion
```go
// Must skip structural tokens, convert to events
for {
    tok := readToken()
    switch tok.Type {
    case token.TComma, token.TColon:
        continue  // Skip
    
    case token.TLCurl:
        state.ProcessToken(tok)
        return EventBeginObject
    
    case token.TString:
        // Complex: key vs value detection
        // ...
    }
}
```

**Challenges**:
- Skip right tokens
- Handle all token types
- Maintain state consistency

**Estimated Complexity**: ⭐⭐⭐⭐⭐ (Hardest)
- ~300-400 lines
- Multiple concerns (buffering, tokenization, state, events)
- Many edge cases
- State synchronization critical

## Comparison

| Aspect | Encoder (Write) | Decoder (Read) |
|--------|----------------|----------------|
| **Determinism** | ✅ We control output | ❌ Don't know input |
| **Buffering** | ✅ Not needed | ❌ Required for partial tokens |
| **State Complexity** | ✅ Simple (we know context) | ❌ Complex (must infer context) |
| **Key/Value Detection** | ✅ We know what we're writing | ❌ Must detect from context |
| **Edge Cases** | ⚠️ Fewer | ❌ Many (partial tokens, EOF, etc.) |
| **Code Reuse** | ✅ Can reuse `encode` package | ⚠️ Can reuse `token.Source` patterns |
| **Estimated Lines** | ~200-300 | ~300-400 |
| **Complexity** | ⭐⭐⭐ Medium | ⭐⭐⭐⭐⭐ Hardest |

## Verdict

**Read Side (Decoder) is Harder**

**Reasons**:
1. ❌ **Non-deterministic input** - Don't know what's coming
2. ❌ **Buffering complexity** - Must handle partial tokens
3. ❌ **State synchronization** - Critical for key/value detection
4. ❌ **More edge cases** - Partial tokens, EOF, empty structures
5. ❌ **Context inference** - Must figure out structure from tokens

**Write Side (Encoder) is Easier**

**Reasons**:
1. ✅ **Deterministic** - We control what we write
2. ✅ **No buffering** - Just write directly
3. ✅ **Simple state** - We know what structure we're in
4. ✅ **Can reuse code** - `encode` package has formatting logic
5. ✅ **Fewer edge cases** - We control the output

## Conclusion

**Hardest Single Functionality**: **Decoder's `ReadEvent()` method** (Read Side)

**Why**: Combines streaming tokenization, buffering, state synchronization, and key/value detection - all non-deterministic concerns.

**Mitigation**: 
- Reuse `token.Source` buffering patterns
- Incremental implementation
- Comprehensive testing
- Start with simpler cases, add complexity gradually
