# Current Multiline String Tokenization

## Current Flow

Looking at `token/mstring.go`:

1. **`mString()`** - Main function
   - Calls `mStringOne()` for each line → returns `TString` token per line
   - Collects multiple `TString` tokens
   - Calls `msMergeToks()` to merge them
   - Returns single `TMString` token + comment tokens

2. **`mStringOne()`** - Processes one line
   - Returns `[]Token` with single `TString` token
   - One token per line

3. **`msMergeToks()`** - Merges tokens
   - Takes multiple `TString` tokens (one per line)
   - Merges into single `TMString` token (all lines)
   - Handles comments (one per line)

## Current Output

**Input**: `|+ "line1\nline2\nline3"`

**Intermediate** (inside `mString()`):
```go
toks := []Token{
    {Type: TString, Bytes: []byte("line1")},   // From mStringOne()
    {Type: TString, Bytes: []byte("line2")},   // From mStringOne()
    {Type: TString, Bytes: []byte("line3")},   // From mStringOne()
}
```

**Final** (after `msMergeToks()`):
```go
toks := []Token{
    {Type: TMString, Bytes: []byte("line1\nline2\nline3")},  // Merged!
    // Plus comment tokens if any
}
```

## Key Insight

**Multiline strings ARE already returned as a single token!**

But the **intermediate processing** creates multiple tokens that get merged.

## Simplification Opportunity

**Current**: Create multiple tokens → Merge into one
**Proposed**: Create single token directly

**Benefits**:
- ✅ Skip intermediate step
- ✅ Simpler `mString()` logic
- ✅ No need for `msMergeToks()` complexity
- ✅ Fewer allocations

## Proposed Change

Instead of:
1. Call `mStringOne()` for each line → get `TString` tokens
2. Collect tokens
3. Call `msMergeToks()` → merge into `TMString`

Do:
1. Scan all lines directly
2. Create single `TMString` token with all lines
3. Handle comments separately

**Code reduction**: ~50-100 lines in `mstring.go`
