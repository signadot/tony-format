# Do We Need Both ReadToken and ProcessToken?

## The Question

Why do we need both `ReadToken()` and `ProcessToken()` on `StreamDecoder`?

## Current Design

```go
type StreamDecoder struct {
    reader io.Reader
    state  *StreamState  // Internal
    // ...
}

// ReadToken reads from reader, tokenizes, updates state
func (d *StreamDecoder) ReadToken() (token.Token, error)

// ProcessToken updates state for pre-tokenized token
func (d *StreamDecoder) ProcessToken(tok token.Token) error
```

## Use Cases for ProcessToken

### Use Case 1: Pre-Tokenized Data

**Scenario**: You already have tokens, want to update state

```go
tokens := []token.Token{...}  // Already tokenized
dec := parse.NewStreamDecoder(nil)

for _, tok := range tokens {
    dec.ProcessToken(tok)  // Update state
    path := dec.CurrentPath()
}
```

**Alternative**: Use `StreamState` directly
```go
tokens := []token.Token{...}
state := parse.NewStreamState()  // Use StreamState directly!

for _, tok := range tokens {
    state.ProcessToken(tok)  // Update state
    path := state.CurrentPath()
}
```

**Verdict**: ⚠️ **Redundant** - can use `StreamState` directly

### Use Case 2: Manual State Control

**Scenario**: You want to manually control when state updates

```go
dec := parse.NewStreamDecoder(reader)

tok, _ := dec.ReadToken()  // Tokenizes but doesn't update state?
// Actually, ReadToken() DOES update state...

// So you'd need to peek, then process manually?
tok := peekToken()
dec.ProcessToken(tok)  // Update state manually
```

**Problem**: `ReadToken()` already updates state, so this doesn't make sense.

**Verdict**: ❌ **Not a valid use case**

### Use Case 3: Testing

**Scenario**: Testing with pre-tokenized tokens

```go
tokens := []token.Token{...}
dec := parse.NewStreamDecoder(nil)

for _, tok := range tokens {
    dec.ProcessToken(tok)
    // Test state queries
}
```

**Alternative**: Use `StreamState` directly
```go
tokens := []token.Token{...}
state := parse.NewStreamState()

for _, tok := range tokens {
    state.ProcessToken(tok)
    // Test state queries
}
```

**Verdict**: ⚠️ **Redundant** - can use `StreamState` directly

## The Real Question

**What is StreamDecoder's purpose?**

1. **Convenience wrapper** that adds tokenization to `StreamState`
2. **If so**: Then `ProcessToken()` doesn't fit - you should use `StreamState` directly for pre-tokenized data

## Option 1: Remove ProcessToken from StreamDecoder

**API**:
```go
type StreamDecoder struct {
    // ...
}

// ReadToken reads from reader, tokenizes, updates state
func (d *StreamDecoder) ReadToken() (token.Token, error)

// NO ProcessToken() method
```

**Usage**:
- **Reading from stream**: Use `StreamDecoder.ReadToken()`
- **Pre-tokenized data**: Use `StreamState.ProcessToken()` directly

**Pros**:
- ✅ Clearer API: `StreamDecoder` = tokenization + state
- ✅ Less API surface
- ✅ Clear separation: `StreamDecoder` for I/O, `StreamState` for state

**Cons**:
- ⚠️ Can't use `StreamDecoder` query methods with pre-tokenized data
- ⚠️ But you can use `StreamState` query methods instead

## Option 2: Keep ProcessToken (Current Design)

**API**:
```go
type StreamDecoder struct {
    // ...
}

func (d *StreamDecoder) ReadToken() (token.Token, error)
func (d *StreamDecoder) ProcessToken(tok token.Token) error
```

**Usage**:
- **Reading from stream**: Use `ReadToken()`
- **Pre-tokenized data**: Use `ProcessToken()` + `StreamDecoder` query methods

**Pros**:
- ✅ Can use `StreamDecoder` query methods with pre-tokenized data
- ✅ Single API for both use cases

**Cons**:
- ⚠️ Adds API surface
- ⚠️ Might be confusing (why two ways to update state?)
- ⚠️ Redundant with `StreamState`

## Analysis: What's the Actual Need?

### Scenario A: Reading from Stream

```go
dec := parse.NewStreamDecoder(reader)
tok, _ := dec.ReadToken()  // ✅ Tokenizes + updates state
path := dec.CurrentPath()   // ✅ Query state
```

**Need**: `ReadToken()` ✅

### Scenario B: Pre-Tokenized Data

**Option B1**: Use `StreamDecoder.ProcessToken()`
```go
dec := parse.NewStreamDecoder(nil)
dec.ProcessToken(tok)  // Update state
path := dec.CurrentPath()  // Query state
```

**Option B2**: Use `StreamState` directly
```go
state := parse.NewStreamState()
state.ProcessToken(tok)  // Update state
path := state.CurrentPath()  // Query state
```

**Question**: Do we need `StreamDecoder` query methods for pre-tokenized data?

**Answer**: Probably not - `StreamState` provides the same query methods!

## Recommendation: Remove ProcessToken

**Rationale**:
1. `StreamDecoder` = convenience wrapper for tokenization
2. If you have tokens already, use `StreamState` directly
3. Simpler API: one way to read (`ReadToken()`), one way to process tokens (`StreamState.ProcessToken()`)
4. Clear separation of concerns

**Updated API**:
```go
// StreamDecoder: Tokenization + State (for io.Reader)
type StreamDecoder struct {
    // ...
}

func (d *StreamDecoder) ReadToken() (token.Token, error)
func (d *StreamDecoder) Depth() int              // Delegates to state
func (d *StreamDecoder) CurrentPath() string     // Delegates to state
// ... other query methods delegate to state

// StreamState: State only (for pre-tokenized tokens)
type StreamState struct {
    // ...
}

func (s *StreamState) ProcessToken(tok token.Token) error
func (s *StreamState) Depth() int
func (s *StreamState) CurrentPath() string
// ... query methods
```

**Usage**:
```go
// Reading from stream
dec := parse.NewStreamDecoder(reader)
tok, _ := dec.ReadToken()
path := dec.CurrentPath()

// Pre-tokenized data
state := parse.NewStreamState()
state.ProcessToken(tok)
path := state.CurrentPath()
```

## Alternative: Keep ProcessToken But Document Clearly

If we keep `ProcessToken()`, we should document:

```go
// ProcessToken processes a pre-tokenized token and updates state.
// Use this when you already have tokens and want to use StreamDecoder's
// query methods. For pre-tokenized data, consider using StreamState
// directly instead.
func (d *StreamDecoder) ProcessToken(tok token.Token) error
```

**But**: This adds complexity and might confuse users.

## Conclusion

**Recommendation**: **Remove `ProcessToken()` from `StreamDecoder`**

**Reasoning**:
- `StreamDecoder` = tokenization wrapper
- Pre-tokenized data → use `StreamState` directly
- Simpler, clearer API
- Less API surface

**If we need it later**: Can always add it back, but start simple.
