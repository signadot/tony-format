# Document Boundary Detection

## The Problem

We need a function that takes a `[]byte` (the snapshot) and an offset (somewhere within a sub-document) and finds:
1. Where the sub-document containing that offset **starts**
2. Where that sub-document **ends**

**Key Constraint**: We don't know where the sub-document starts - we only have an offset somewhere within it.

**Use Cases**:
1. **Sub-document parsing**: Read sub-document from snapshot given an approximate offset
2. **Snapshot reading**: Find exact boundaries of a sub-document to read it
3. **Offset tracking**: When we have an offset, find the complete sub-document boundaries

## The Challenge

In the storage format scenario:
- Snapshot is a giant Tony document (1GB)
- We have an offset where we think a sub-document is (e.g., offset 12345)
- But we don't know:
  - Where the sub-document actually starts (might be before offset)
  - Where the sub-document ends (might be after offset)
  - What the sub-document structure is (object? array? value?)

**We need to scan backwards and forwards from the offset to find boundaries.**

## Approach: Find Boundaries Around Offset

### Wire Format (Bracketed)

**Algorithm**: Scan backwards to find opening bracket, forwards to find closing bracket.

```go
// FindDocumentBoundaryWire finds the start and end of a document containing offset.
// Returns (start, end) such that data[start:end] contains exactly the document.
func FindDocumentBoundaryWire(data []byte, offset int) (start, end int, err error) {
    if offset < 0 || offset >= len(data) {
        return 0, 0, fmt.Errorf("offset out of range")
    }
    
    // Step 1: Scan backwards to find the opening bracket that contains offset
    start = offset
    curlDepth := 0
    squareDepth := 0
    
    // Track depth as we scan backwards
    for start >= 0 {
        switch data[start] {
        case '}':
            curlDepth++
        case '{':
            curlDepth--
            if curlDepth == 0 && squareDepth == 0 {
                // Found the opening bracket - this is where document starts
                // Skip leading whitespace
                for start > 0 && (data[start-1] == ' ' || data[start-1] == '\n' || data[start-1] == '\t') {
                    start--
                }
                goto foundStart
            }
        case ']':
            squareDepth++
        case '[':
            squareDepth--
            if curlDepth == 0 && squareDepth == 0 {
                // Found the opening bracket
                for start > 0 && (data[start-1] == ' ' || data[start-1] == '\n' || data[start-1] == '\t') {
                    start--
                }
                goto foundStart
            }
        case '"':
            // Skip string literal (scan backwards)
            start--
            for start >= 0 && data[start] != '"' {
                if start > 0 && data[start] == '\\' {
                    start-- // Skip escaped character
                }
                start--
            }
        }
        start--
    }
    
    return 0, 0, fmt.Errorf("could not find document start")
    
foundStart:
    // Step 2: Scan forwards from start to find matching closing bracket
    end = start
    curlDepth = 0
    squareDepth = 0
    
    // Determine starting bracket type
    if data[start] == '{' {
        curlDepth = 1
    } else if data[start] == '[' {
        squareDepth = 1
    } else {
        return 0, 0, fmt.Errorf("invalid start position")
    }
    
    end++
    
    for end < len(data) {
        switch data[end] {
        case '{':
            curlDepth++
        case '}':
            curlDepth--
            if curlDepth == 0 && squareDepth == 0 {
                // Found matching closing bracket
                end++ // Include closing bracket
                // Skip trailing whitespace/comments
                for end < len(data) && (data[end] == ' ' || data[end] == '\n' || data[end] == '\t' || data[end] == ',') {
                    end++
                }
                return start, end, nil
            }
        case '[':
            squareDepth++
        case ']':
            squareDepth--
            if curlDepth == 0 && squareDepth == 0 {
                // Found matching closing bracket
                end++
                for end < len(data) && (data[end] == ' ' || data[end] == '\n' || data[end] == '\t' || data[end] == ',') {
                    end++
                }
                return start, end, nil
            }
        case '"':
            // Skip string literals
            end++
            for end < len(data) && data[end] != '"' {
                if data[end] == '\\' {
                    end++
                }
                end++
            }
        case '\'':
            // Skip single-quoted strings
            end++
            for end < len(data) && data[end] != '\'' {
                if data[end] == '\\' {
                    end++
                }
                end++
            }
        }
        end++
    }
    
    return 0, 0, fmt.Errorf("unclosed bracket")
}
```

### Bracketed Format (Tony with brackets)

**Characteristics**:
- Similar to wire format
- Uses `{` `}` for objects, `[` `]` for arrays
- Can have both types in same document

**Algorithm**: Same as wire format - scan backwards/forwards from offset.

```go
func FindDocumentBoundaryBracketed(data []byte, offset int) (start, end int, err error) {
    // Same algorithm as FindDocumentBoundaryWire
    // Handles both { } and [ ] brackets
    return FindDocumentBoundaryWire(data, offset)
}
    if len(data) == 0 {
        return 0, fmt.Errorf("empty data")
    }
    
    // Find opening bracket
    start := 0
    for start < len(data) && (data[start] == ' ' || data[start] == '\n' || data[start] == '\t') {
        start++
    }
    
    if start >= len(data) {
        return 0, fmt.Errorf("no document found")
    }
    
    // Track both types of brackets
    curlDepth := 0  // { }
    squareDepth := 0 // [ ]
    
    i := start
    
    // Determine starting bracket type
    switch data[start] {
    case '{':
        curlDepth = 1
    case '[':
        squareDepth = 1
    default:
        return 0, fmt.Errorf("document must start with { or [")
    }
    
    i++
    
    for i < len(data) {
        switch data[i] {
        case '{':
            curlDepth++
        case '}':
            curlDepth--
            if curlDepth == 0 && squareDepth == 0 {
                // Found matching closing bracket, document complete
                end := i + 1
                for end < len(data) && (data[end] == ' ' || data[end] == '\n' || data[end] == '\t' || data[end] == ',') {
                    end++
                }
                return end, nil
            }
        case '[':
            squareDepth++
        case ']':
            squareDepth--
            if curlDepth == 0 && squareDepth == 0 {
                // Found matching closing bracket, document complete
                end := i + 1
                for end < len(data) && (data[end] == ' ' || data[end] == '\n' || data[end] == '\t' || data[end] == ',') {
                    end++
                }
                return end, nil
            }
        case '"':
            // Skip string literals
            i++
            for i < len(data) && data[i] != '"' {
                if data[i] == '\\' {
                    i++
                }
                i++
            }
        case '\'':
            // Skip single-quoted strings
            i++
            for i < len(data) && data[i] != '\'' {
                if data[i] == '\\' {
                    i++
                }
                i++
            }
        }
        i++
    }
    
    return 0, fmt.Errorf("unclosed brackets")
}
```

### Indentation-Based Format (Tony/YAML)

**Algorithm**: Tokenize, find token containing offset, scan backwards/forwards to find boundaries.

```go
// FindDocumentBoundaryIndented finds the start and end of a document containing offset.
// Returns (start, end) such that data[start:end] contains exactly the document.
func FindDocumentBoundaryIndented(data []byte, offset int, baseIndent int) (start, end int, err error) {
    if offset < 0 || offset >= len(data) {
        return 0, 0, fmt.Errorf("offset out of range")
    }
    
    // Tokenize the data
    toks, err := token.Tokenize(nil, data)
    if err != nil {
        return 0, 0, err
    }
    
    if len(toks) == 0 {
        return 0, 0, fmt.Errorf("no tokens found")
    }
    
    // Find token containing the offset
    tokenIdx := -1
    for i, tok := range toks {
        tokStart := tok.Pos.Start()
        tokEnd := tok.Pos.End()
        if offset >= tokStart && offset < tokEnd {
            tokenIdx = i
            break
        }
    }
    
    if tokenIdx == -1 {
        return 0, 0, fmt.Errorf("offset not found in tokens")
    }
    
    // Determine base indent
    if baseIndent < 0 {
        // Auto-detect: find first content token's indent level
        for i := 0; i < len(toks); i++ {
            if toks[i].Type == token.TIndent {
                baseIndent = len(toks[i].Bytes)
                break
            }
        }
        if baseIndent < 0 {
            baseIndent = 0
        }
    }
    
    // Step 1: Scan backwards to find document start
    // Document starts when we find an indent token at base level (or start of data)
    startTokenIdx := tokenIdx
    for startTokenIdx >= 0 {
        tok := &toks[startTokenIdx]
        if tok.Type == token.TIndent {
            indentLevel := len(tok.Bytes)
            if indentLevel <= baseIndent {
                // Found base level - document starts at next token (or this token if it's the first)
                if startTokenIdx > 0 {
                    startTokenIdx++
                }
                break
            }
        }
        startTokenIdx--
    }
    
    if startTokenIdx < 0 {
        startTokenIdx = 0
    }
    
    start = toks[startTokenIdx].Pos.Start()
    
    // Step 2: Scan forwards to find document end
    // Document ends when we find an indent token at base level (or end of data)
    endTokenIdx := tokenIdx
    for endTokenIdx < len(toks) {
        tok := &toks[endTokenIdx]
        if tok.Type == token.TIndent {
            indentLevel := len(tok.Bytes)
            if indentLevel <= baseIndent {
                // Found base level - document ends before this token
                end = tok.Pos.Start()
                return start, end, nil
            }
        }
        endTokenIdx++
    }
    
    // Document extends to end of data
    end = len(data)
    return start, end, nil
}
```

## Unified Function

```go
// FindDocumentBoundary finds the start and end of a document containing offset.
// Returns (start, end) such that data[start:end] contains exactly the document.
func FindDocumentBoundary(data []byte, offset int, f format.Format, baseIndent int) (start, end int, err error) {
    if offset < 0 || offset >= len(data) {
        return 0, 0, fmt.Errorf("offset out of range")
    }
    
    switch f {
    case format.WireFormat, format.JSONFormat:
        return FindDocumentBoundaryWire(data, offset)
    case format.TonyFormat:
        // Check if document uses brackets
        if hasBrackets(data, offset) {
            return FindDocumentBoundaryBracketed(data, offset)
        }
        // Otherwise, use indentation-based
        return FindDocumentBoundaryIndented(data, offset, baseIndent)
    case format.YAMLFormat:
        return FindDocumentBoundaryIndented(data, offset, baseIndent)
    default:
        return 0, 0, fmt.Errorf("unsupported format: %v", f)
    }
}

// Helper to check if document uses brackets (scan around offset)
func hasBrackets(data []byte, offset int) bool {
    // Scan backwards from offset to find nearest bracket
    for i := offset; i >= 0 && i >= offset-100; i-- { // Check up to 100 bytes back
        if data[i] == '{' || data[i] == '[' {
            return true
        }
        if data[i] == '}' || data[i] == ']' {
            return true
        }
    }
    // Scan forwards from offset
    for i := offset; i < len(data) && i < offset+100; i++ { // Check up to 100 bytes forward
        if data[i] == '{' || data[i] == '[' {
            return true
        }
        if data[i] == '}' || data[i] == ']' {
            return true
        }
    }
    return false
}
```

## Usage for Sub-Document Reading

```go
// Read sub-document from offset (offset is approximate, within the sub-document)
func readSubDocumentFromOffset(reader io.ReaderAt, snapshotOffset int64, subDocOffset int64, 
                                maxSize int64, f format.Format) (*ir.Node, error) {
    // Read chunk around the offset (with buffer for scanning backwards/forwards)
    // We need buffer before offset (to scan backwards) and after (to scan forwards)
    bufferSize := maxSize + 1024 // Extra buffer for boundary detection
    readOffset := snapshotOffset + subDocOffset - 512 // Start 512 bytes before offset
    if readOffset < snapshotOffset {
        readOffset = snapshotOffset
    }
    
    buf := make([]byte, bufferSize)
    n, err := reader.ReadAt(buf, readOffset)
    if err != nil && err != io.EOF {
        return nil, err
    }
    
    // Calculate offset within buffer
    offsetInBuf := int(subDocOffset - (readOffset - snapshotOffset))
    if offsetInBuf < 0 {
        offsetInBuf = 0
    }
    if offsetInBuf >= n {
        return nil, fmt.Errorf("offset beyond buffer")
    }
    
    // Find document boundaries around offset
    start, end, err := FindDocumentBoundary(buf[:n], offsetInBuf, f, -1) // -1 = auto-detect
    if err != nil {
        return nil, err
    }
    
    // Adjust start/end to absolute positions
    absStart := readOffset + int64(start)
    absEnd := readOffset + int64(end)
    
    // Read the exact document
    docSize := absEnd - absStart
    docBuf := make([]byte, docSize)
    _, err = reader.ReadAt(docBuf, absStart)
    if err != nil {
        return nil, err
    }
    
    // Parse the document
    return Parse(docBuf)
}
```

**Key Points**:
1. **Read buffer around offset** - need space to scan backwards and forwards
2. **Find boundaries** - scan from offset to find start and end
3. **Read exact document** - read only the bytes between boundaries
4. **Parse document** - parse just that chunk

## Implementation Considerations

### Wire/Bracketed Format

**Pros**:
- ✅ Simple bracket counting
- ✅ Fast (single pass)
- ✅ No tokenization needed

**Cons**:
- ⚠️ Must handle string literals (skip brackets inside strings)
- ⚠️ Must handle escaped characters

### Indentation-Based Format

**Pros**:
- ✅ Accurate (uses tokenizer)
- ✅ Handles all edge cases

**Cons**:
- ⚠️ Requires tokenization (more overhead)
- ⚠️ Need base indent level (or auto-detect)

### Optimization

For wire/bracketed format, we can optimize by:
1. **Simple bracket counter** - fast path for common case
2. **String detection** - handle strings correctly
3. **No tokenization needed** - very fast

For indentation-based format:
1. **Tokenize incrementally** - only tokenize what we need
2. **Cache base indent** - don't recompute
3. **Early exit** - stop when we find boundary

## Recommendation

**Implement all three variants**:
1. `FindDocumentBoundaryWire` - for wire format (fastest)
2. `FindDocumentBoundaryBracketed` - for bracketed format (fast)
3. `FindDocumentBoundaryIndented` - for indentation-based (accurate)

**Unified function** `FindDocumentBoundary` that:
- Detects format (or takes as parameter)
- Routes to appropriate variant
- Handles base indent (auto-detect or parameter)

**For our use case** (sub-documents in snapshots):
- Wire format: Use bracket counting (fast)
- Tony format: Check for brackets first, fall back to indentation
- YAML format: Use indentation-based
