# Streaming Parse Design: Adding Streaming Support to Tony Parser

## Current Architecture

**Current Flow**:
```
Parse([]byte) 
  → token.Tokenize([]byte) → []Token
  → token.Balance([]Token) → []Token  
  → parse.parseBalanced([]Token) → *ir.Node
```

**Key Characteristics**:
- All operations work on slices (in-memory)
- `Tokenize` takes `[]byte`, returns `[]Token`
- `Balance` takes `[]Token`, returns `[]Token`
- `Parse` takes `[]byte`, returns `*ir.Node`
- Everything is loaded into memory first

## Goal: Add Streaming Support

**Requirements**:
1. **Don't break existing API** - keep `Parse([]byte)` working
2. **Add streaming versions** - new functions that work with `io.Reader`
3. **Reuse existing logic** - leverage tokenization, balancing, parsing logic
4. **Support seeking** - can start parsing from arbitrary offset

## Design Approach

### Option A: Streaming Tokenizer → Streaming Balance → Streaming Parse

**Flow**:
```
ParseFromReader(io.Reader)
  → token.TokenizeStream(io.Reader) → <-chan Token (or iterator)
  → token.BalanceStream(<-chan Token) → <-chan Token
  → parse.parseBalancedStream(<-chan Token) → *ir.Node
```

**Pros**:
- Fully streaming
- Minimal memory usage
- Natural fit for large documents

**Cons**:
- Requires rewriting balance/parse logic
- More complex implementation
- Harder to maintain (two code paths)

### Option B: Buffered Streaming (Recommended)

**Flow**:
```
ParseFromReader(io.Reader, offset, size)
  → Read chunk from reader (io.SectionReader)
  → token.Tokenize([]byte) → []Token (existing)
  → token.Balance([]Token) → []Token (existing)
  → parse.parseBalanced([]Token) → *ir.Node (existing)
```

**Pros**:
- ✅ **Reuses existing code** - no rewrite needed
- ✅ **Simple** - just read chunk, then use existing functions
- ✅ **Maintainable** - one code path
- ✅ **Works for sub-documents** - read only needed chunk

**Cons**:
- Must read entire sub-document into memory (but sub-docs are small)
- Not fully streaming (but acceptable for our use case)

### Option C: Hybrid - Streaming Tokenizer, Buffered Balance/Parse

**Flow**:
```
ParseFromReader(io.Reader, offset, size)
  → token.TokenizeStream(io.Reader, offset, size) → []Token (streaming tokenize)
  → token.Balance([]Token) → []Token (existing)
  → parse.parseBalanced([]Token) → *ir.Node (existing)
```

**Pros**:
- Streaming tokenization (can handle large inputs)
- Reuses balance/parse logic
- Good middle ground

**Cons**:
- Still need all tokens in memory for balance/parse
- More complex than Option B

## Recommendation: Option B (Buffered Streaming)

**Rationale**:
1. **Sub-documents are small** - we're reading 1KB sub-docs, not 1GB documents
2. **Reuses existing code** - no rewrite needed
3. **Simple** - just read chunk, parse it
4. **Maintainable** - one code path

**Implementation**:
```go
// ParseFromReader reads a chunk from reader and parses it
func ParseFromReader(reader io.ReaderAt, offset int64, size int64, opts ...ParseOption) (*ir.Node, error) {
    // Create section reader for the chunk
    sectionReader := io.NewSectionReader(reader, offset, size)
    
    // Read chunk into buffer
    buf := make([]byte, size)
    n, err := sectionReader.Read(buf)
    if err != nil && err != io.EOF {
        return nil, err
    }
    
    // Use existing Parse function
    return Parse(buf[:n], opts...)
}
```

**For Sub-Document Reading**:
```go
// Read sub-document from snapshot
func readSubDocumentFromOffset(reader io.ReaderAt, snapshotOffset int64, ref SubDocRef) (*ir.Node, error) {
    // Calculate absolute offset
    absoluteOffset := snapshotOffset + snapshotEntryHeaderSize + ref.Offset
    
    // Parse from reader at offset
    return ParseFromReader(reader, absoluteOffset, ref.Size)
}
```

## Alternative: If We Need True Streaming

If we later need true streaming (for very large sub-documents), we can add:

### Streaming Tokenizer

```go
// TokenizeStream tokenizes from a reader, yielding tokens via channel
func TokenizeStream(reader io.Reader, opts ...TokenizeOption) (<-chan Token, <-chan error) {
    tokenChan := make(chan Token, 100) // Buffered channel
    errChan := make(chan error, 1)
    
    go func() {
        defer close(tokenChan)
        defer close(errChan)
        
        // Read in chunks, tokenize incrementally
        buf := make([]byte, 64*1024) // 64KB buffer
        for {
            n, err := reader.Read(buf)
            if err != nil && err != io.EOF {
                errChan <- err
                return
            }
            if n == 0 {
                break
            }
            
            // Tokenize chunk
            toks, err := Tokenize(nil, buf[:n], opts...)
            if err != nil {
                errChan <- err
                return
            }
            
            // Send tokens
            for _, tok := range toks {
                tokenChan <- tok
            }
            
            if err == io.EOF {
                break
            }
        }
    }()
    
    return tokenChan, errChan
}
```

**But**: This is more complex and may not be needed if sub-documents are small.

## Implementation Plan

### Phase 1: Simple Buffered Approach (Recommended)

1. **Add `ParseFromReader`**:
   ```go
   func ParseFromReader(reader io.ReaderAt, offset int64, size int64, opts ...ParseOption) (*ir.Node, error)
   ```

2. **Implementation**:
   - Use `io.NewSectionReader` to read chunk
   - Read into buffer
   - Call existing `Parse([]byte)` function
   - Done!

3. **Benefits**:
   - ✅ Simple (few lines of code)
   - ✅ Reuses all existing logic
   - ✅ Works for sub-documents (small chunks)
   - ✅ No changes to existing code

### Phase 2: If Needed - Streaming Tokenizer

Only if we need to handle very large sub-documents (> 100MB):

1. Add `TokenizeStream` function
2. Keep existing `Tokenize` for backward compatibility
3. Use streaming tokenizer for large inputs

## Key Insight

**For our use case** (reading 1KB sub-documents from 1GB snapshot):
- ✅ **Buffered approach is sufficient** - 1KB fits easily in memory
- ✅ **No need for true streaming** - sub-docs are small
- ✅ **Simple implementation** - just read chunk, parse it
- ✅ **Reuses existing code** - no rewrite needed

**If sub-documents become large** (> 10MB), we can add streaming tokenizer later.

## Conclusion

**Recommendation**: **Option B (Buffered Streaming)**

**Implementation**:
```go
func ParseFromReader(reader io.ReaderAt, offset int64, size int64, opts ...ParseOption) (*ir.Node, error) {
    sectionReader := io.NewSectionReader(reader, offset, size)
    buf := make([]byte, size)
    n, err := sectionReader.Read(buf)
    if err != nil && err != io.EOF {
        return nil, err
    }
    return Parse(buf[:n], opts...)
}
```

**Benefits**:
- ✅ Simple (few lines)
- ✅ Reuses existing code
- ✅ Works for our use case (small sub-docs)
- ✅ No changes to existing parser

**Future**: Can add true streaming if needed, but buffered approach is sufficient for now.
