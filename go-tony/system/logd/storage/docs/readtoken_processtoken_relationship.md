# ReadToken vs ProcessToken Relationship

## The Question

What is the relationship between `ReadToken()` and `ProcessToken()` in StreamDecoder?

## Current API Design

```go
// ReadToken reads the next token from the stream.
// Tokenizes internally, updates state automatically.
func (d *StreamDecoder) ReadToken() (token.Token, error)

// ProcessToken processes a token for pre-tokenized data.
// Updates state/path tracking. Use this if you already have tokens.
// Delegates to internal StreamState.
func (d *StreamDecoder) ProcessToken(tok token.Token) error
```

## The Relationship

### Option 1: ReadToken Calls ProcessToken Internally

**Implementation**:
```go
func (d *StreamDecoder) ReadToken() (token.Token, error) {
    // Tokenize from reader
    tok, err := d.tokenizeNext()
    if err != nil {
        return token.Token{}, err
    }
    
    // Update state automatically
    if err := d.ProcessToken(tok); err != nil {
        return token.Token{}, err
    }
    
    return tok, nil
}

func (d *StreamDecoder) ProcessToken(tok token.Token) error {
    // Delegate to internal StreamState
    return d.state.ProcessToken(tok)
}
```

**Relationship**: `ReadToken()` = tokenization + `ProcessToken()`

**Use Cases**:
- `ReadToken()` - when reading from `io.Reader` (most common)
- `ProcessToken()` - when you already have tokens (pre-tokenized data)

### Option 2: ReadToken Does Everything, ProcessToken is Separate

**Implementation**:
```go
func (d *StreamDecoder) ReadToken() (token.Token, error) {
    // Tokenize from reader
    tok, err := d.tokenizeNext()
    if err != nil {
        return token.Token{}, err
    }
    
    // Update state directly (not via ProcessToken)
    d.state.ProcessToken(tok)
    
    return tok, nil
}

func (d *StreamDecoder) ProcessToken(tok token.Token) error {
    // Separate method for pre-tokenized data
    return d.state.ProcessToken(tok)
}
```

**Relationship**: Both update state, but `ReadToken()` also tokenizes

**Use Cases**:
- `ReadToken()` - when reading from `io.Reader`
- `ProcessToken()` - when you already have tokens

## Recommended Design: Option 1

**Why**: 
- ✅ Clear separation: `ReadToken()` = tokenization + state update
- ✅ `ProcessToken()` is reusable (can be called independently)
- ✅ Consistent: state always updated via `ProcessToken()`

**Implementation**:
```go
type StreamDecoder struct {
    reader io.Reader
    state  *StreamState
    // ... tokenization buffer/state
}

func (d *StreamDecoder) ReadToken() (token.Token, error) {
    // 1. Tokenize from reader
    tok, err := d.tokenizeNext()
    if err != nil {
        return token.Token{}, err
    }
    
    // 2. Update state (delegates to ProcessToken)
    if err := d.ProcessToken(tok); err != nil {
        return token.Token{}, err
    }
    
    // 3. Return token
    return tok, nil
}

func (d *StreamDecoder) ProcessToken(tok token.Token) error {
    // Delegate to internal StreamState
    return d.state.ProcessToken(tok)
}
```

## Usage Patterns

### Pattern 1: Reading from Stream (Most Common)

```go
dec := parse.NewStreamDecoder(reader)

for {
    tok, err := dec.ReadToken()  // Tokenizes AND updates state
    if err == io.EOF {
        break
    }
    
    // State is already updated
    path := dec.CurrentPath()  // ✅ Works immediately
    depth := dec.Depth()       // ✅ Works immediately
}
```

### Pattern 2: Pre-Tokenized Data

```go
// You already have tokens
tokens := []token.Token{...}

dec := parse.NewStreamDecoder(nil)  // No reader needed

for _, tok := range tokens {
    // Update state manually
    if err := dec.ProcessToken(tok); err != nil {
        return err
    }
    
    // State is updated
    path := dec.CurrentPath()  // ✅ Works
    depth := dec.Depth()       // ✅ Works
}
```

### Pattern 3: Mixed Usage

```go
dec := parse.NewStreamDecoder(reader)

// Read some tokens
tok1, _ := dec.ReadToken()  // Tokenizes + updates state
tok2, _ := dec.ReadToken()  // Tokenizes + updates state

// Process some pre-tokenized tokens
preTokens := []token.Token{...}
for _, tok := range preTokens {
    dec.ProcessToken(tok)  // Just updates state
}

// Continue reading
tok3, _ := dec.ReadToken()  // Tokenizes + updates state
```

## Key Points

1. **ReadToken()**:
   - Reads from `io.Reader`
   - Tokenizes internally
   - Updates state automatically (via `ProcessToken()`)
   - Returns token

2. **ProcessToken()**:
   - Takes already-tokenized token
   - Updates state only
   - No I/O involved
   - Useful for pre-tokenized data

3. **Relationship**:
   - `ReadToken()` = tokenization + `ProcessToken()`
   - `ProcessToken()` is the lower-level state update method
   - Both update the same internal state

## Alternative: Remove ProcessToken?

**Question**: Do we need `ProcessToken()` at all?

**Arguments for keeping**:
- ✅ Useful for pre-tokenized data
- ✅ Allows manual state control
- ✅ Clear separation of concerns

**Arguments for removing**:
- ⚠️ Adds API surface
- ⚠️ Most users will only use `ReadToken()`
- ⚠️ Can always tokenize first, then use `ReadToken()`

**Recommendation**: **Keep `ProcessToken()`**
- It's a simple delegation to `StreamState.ProcessToken()`
- Useful for flexibility (pre-tokenized data)
- Minimal implementation cost

## Summary

**Relationship**:
```
ReadToken() = tokenize() + ProcessToken()
```

**ReadToken()**:
- Tokenizes from `io.Reader`
- Calls `ProcessToken()` internally
- Updates state automatically

**ProcessToken()**:
- Updates state only
- No tokenization
- Useful for pre-tokenized data

**Both update the same internal state** - you can query `CurrentPath()`, `Depth()`, etc. after either call.
