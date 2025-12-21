# Analysis: Simplifying Multiline String Tokenization

## Current State

### How Multiline Strings Are Tokenized

Looking at `tokenize_one.go` line 133:
```go
// multiline enabled string - returns multiple tokens
toks, off, err := mString(d[i:], absOffset, indent, posDoc)
```

**Current behavior**: 
- `mString()` creates multiple `TString` tokens internally (one per line)
- Then `msMergeToks()` merges them into **single `TMString` token**
- **Final output**: Single token (already!)
- **But**: Complex intermediate processing

### What mString Actually Does

From `mstring.go`:
1. Calls `mStringOne()` for each line → returns `TString` token per line
2. Collects multiple `TString` tokens in array
3. Calls `msMergeToks()` → merges into single `TMString` token
4. Handles comments (one per line)

**Current flow**:
```go
// Intermediate (inside mString)
toks := []Token{
    {Type: TString, Bytes: []byte("line1")},   // From mStringOne()
    {Type: TString, Bytes: []byte("line2")},   // From mStringOne()
    {Type: TString, Bytes: []byte("line3")},   // From mStringOne()
}

// Final (after msMergeToks)
toks := []Token{
    {Type: TMString, Bytes: []byte("line1\nline2\nline3")},  // Single token!
    // Plus comment tokens
}
```

### Impact on Streaming

**Current complexity**:
1. `tokenizeOne()` can return multiple tokens for a single multiline string
2. `StreamDecoder` must handle batches of tokens
3. State updates happen for each line token
4. Path tracking updates for each line token (even though it's one value)

**Example**:
```go
// Input: |+ "line1\nline2\nline3"
// Returns: [TMString("line1"), TMString("line2"), TMString("line3")]
// StreamDecoder processes 3 tokens, updates state 3 times
```

## Proposed: Simplify Intermediate Processing

### Change

**Current**: Create multiple tokens → Merge into one
**Proposed**: Create single token directly

Modify `mString()` to create the single `TMString` token directly, skipping the intermediate step:

```go
// Current: Create multiple TString tokens, then merge
toks := []Token{
    {Type: TString, Bytes: []byte("line1")},
    {Type: TString, Bytes: []byte("line2")},
    {Type: TString, Bytes: []byte("line3")},
}
toks = msMergeToks(toks)  // Merge into single TMString

// Proposed: Create single TMString token directly
strBytes := []byte("line1\nline2\nline3")
toks := []Token{
    {Type: TMString, Bytes: strBytes},
}
```

### Implementation Changes

**In `token/mstring.go`**:
- Skip `mStringOne()` per-line processing
- Scan all lines directly, build single `TMString` token
- Eliminate `msMergeToks()` complexity
- Still handle comments (one per line)

**In `token/tokenize_one.go`**:
- No changes needed (already returns `[]Token`)

**In `token/types.go`**:
- `Token.String()` already handles multiline strings correctly
- No changes needed

## Benefits

### 1. Simpler Tokenization

**Before**:
```go
// mString() returns multiple tokens
toks := []Token{
    {Type: TMString, Bytes: []byte("line1")},
    {Type: TMString, Bytes: []byte("line2")},
    {Type: TMString, Bytes: []byte("line3")},
}
```

**After**:
```go
// mString() returns single token
toks := []Token{
    {Type: TMString, Bytes: []byte("line1\nline2\nline3")},
}
```

**Code reduction**: ~50-100 lines in `mstring.go` (no need to split into multiple tokens)

### 2. Simpler StreamDecoder

**Current**: `tokenizeOne()` already returns single token for multiline strings (after merging)

**But**: The intermediate complexity in `mString()` makes it harder to understand and maintain.

**After simplification**:
- `mString()` creates single token directly
- No intermediate merging step
- Clearer code flow
- Same final result, simpler implementation

**Code reduction**: ~50-100 lines in `mstring.go` (eliminate `msMergeToks()` complexity)

### 3. Simpler Code Flow

**Current**:
```go
// Complex flow with intermediate step
func mString(...) {
    for each line {
        toks := mStringOne(...)  // Returns TString token
        res = append(res, toks...)
    }
    return msMergeToks(res), ...  // Merge into TMString
}

func msMergeToks(toks []Token) []Token {
    // Complex merging logic
    // Handle comments
    // Pad comments to match string count
}
```

**After**:
```go
// Direct creation
func mString(...) {
    strBytes := []byte{}
    comments := []Token{}
    for each line {
        lineBytes := scanLine(...)
        strBytes = append(strBytes, '\n')
        strBytes = append(strBytes, lineBytes...)
        comment := scanComment(...)
        comments = append(comments, comment)
    }
    return []Token{
        {Type: TMString, Bytes: strBytes},
        ...comments,
    }, ...
}
```

**Benefits**:
- Clearer code flow
- No intermediate merging step
- Easier to understand
- Fewer allocations

## Implementation Details

### Change mString() Signature

**Current**:
```go
func mString(d []byte, absOffset, indent int, posDoc *PosDoc) ([]Token, int, error)
// Returns multiple tokens (one per line)
```

**Proposed**:
```go
func mString(d []byte, absOffset, indent int, posDoc *PosDoc) ([]Token, int, error)
// Returns single token (entire multiline string)
```

### Token.Bytes Format

**Current**: Each token contains one line
```go
Token{Type: TMString, Bytes: []byte("line1")}
Token{Type: TMString, Bytes: []byte("line2")}
```

**Proposed**: Single token contains all lines
```go
Token{Type: TMString, Bytes: []byte("line1\nline2\nline3")}
```

**Note**: `Token.String()` already handles this correctly (splits on `\n`)

### Streaming Considerations

**Current**: `mString()` can return `io.EOF` if needs more buffer (for streaming)

**Proposed**: Same behavior - can still return `io.EOF` if needs more buffer
- Just returns single token instead of multiple
- Streaming logic unchanged

## Code Impact

### Files to Modify

1. **`token/mstring.go`** (~180 lines):
   - Eliminate `mStringOne()` per-line processing
   - Eliminate `msMergeToks()` merging logic
   - Create single `TMString` token directly
   - Still handle comments (one per line)
   - **Estimated reduction**: ~50-100 lines

2. **`token/tokenize_one.go`**:
   - No changes needed (already returns `[]Token`)

3. **`parse/stream_decoder.go`**:
   - No changes needed (already handles single token)

### Total Reduction

**Estimated**: ~50-100 lines eliminated in `mstring.go`

## Potential Issues

### 1. Backward Compatibility

**Question**: Does any code depend on the intermediate multiple-token step?

**Analysis**:
- Final output is already single token (after merging)
- No code should depend on intermediate state
- `Token.String()` handles single `TMString` token correctly
- `parse.Parse()` processes tokens sequentially

**Risk**: Very low - we're just simplifying the implementation, not changing the output

### 2. Memory Usage

**Before**: Multiple small tokens (one per line)
**After**: Single larger token (all lines)

**Impact**: 
- Slightly more memory per token
- But fewer tokens overall
- Net: Probably similar or better

### 3. Streaming Buffer Size

**Question**: Do we need larger buffers for multiline strings?

**Analysis**:
- Multiline strings are already buffered until complete
- Single token doesn't change buffering requirements
- Still need to buffer until we see the end marker

**Impact**: None - buffering logic unchanged

## Recommendation

**YES, simplify multiline string tokenization!**

**Benefits**:
- ✅ Simpler `mString()` implementation (~50-100 lines saved)
- ✅ Eliminate `msMergeToks()` complexity
- ✅ Clearer code flow (direct creation vs. merge)
- ✅ Fewer allocations (no intermediate tokens)
- ✅ Easier to understand and maintain

**Risks**:
- ⚠️ Need to preserve comment handling (one per line)
- ⚠️ Need to test thoroughly
- ⚠️ Need to handle streaming mode correctly

**Implementation**:
1. Refactor `mString()` to create single `TMString` token directly
2. Eliminate `msMergeToks()` function
3. Simplify `mStringOne()` or eliminate it
4. Preserve comment handling logic
5. Test thoroughly (especially streaming mode)

**Estimated savings**: ~50-100 lines in `mstring.go`

**Note**: Final output is already single token, so this is purely an implementation simplification, not an API change.
