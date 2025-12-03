# Token Streaming Impact Analysis: Storage Redesign

## Executive Summary

The token streaming API (`TokenSource` and `TokenSink`) **enables critical optimizations** for the storage redesign, particularly for the controller use case with large snapshots and small sub-documents. The streaming API directly addresses key requirements identified in the design documents:

1. ✅ **Streaming reads** - Read sub-documents from offsets without loading entire snapshots
2. ✅ **Offset tracking** - Track byte positions for inverted index and path indexing
3. ✅ **Path detection** - Identify node boundaries during encoding for sub-document indexing
4. ✅ **Memory efficiency** - Process large documents incrementally without full memory load

**Impact Level**: **CRITICAL** - The streaming API is essential for implementing the inverted index sub-document approach and path indexing optimizations.

## Storage System Requirements

### From Design Documents

The storage redesign has identified several critical requirements that depend on streaming capabilities:

#### 1. Inverted Index with Sub-Documents (`inverted_index_subdocuments.md`)

**Requirement**: Break large snapshots (1GB+) into sub-documents (1KB each) and index them.

**Key Constraints**:
- ✅ **Cannot load entire snapshot into memory** - Must process incrementally/streaming
- ✅ **Streaming writes** - Write sub-docs as we process diffs
- ✅ **Streaming reads** - Read sub-docs from offsets using section readers
- ✅ **Tokenization from stream** - Parse wire format from stream, not memory buffer

**Current Status**: Design documents reference `tokenizeWireFormatStream(reader)` and `parseNodeFromTokens(tokens)` - **these are exactly what TokenSource provides**.

#### 2. Snapshot Path Indexing (`snapshot_path_indexing.md`)

**Requirement**: Index paths within snapshots at byte offsets to read only specific paths (10^6:1 ratio benefit).

**Key Requirements**:
- ✅ **Wire format + tokenization** - Tokenize Tony wire format to identify structure
- ✅ **Bracket counting** - Count brackets to find document boundaries
- ✅ **Offset tracking** - Track offsets as we tokenize, accounting for encoding
- ✅ **Seeking support** - Can seek to specific paths within document

**Current Status**: Design documents reference `tokenizeWireFormat(reader)` and offset tracking - **TokenSource provides tokenization, TokenSink provides offset tracking**.

#### 3. Streaming Writer Design (`streaming_writer_design.md`)

**Requirement**: Position-aware writer that tracks byte offsets and detects node boundaries.

**Key Requirements**:
- ✅ **Track byte positions** - Know exactly where we are in output stream
- ✅ **Identify sub-document boundaries** - Detect when complete sub-document written
- ✅ **Record precise offsets** - Store exact start/end positions in inverted index
- ✅ **Node start detection** - Detect when nodes start for callback

**Current Status**: Design documents propose `StreamingWriter` with `Offset()` and `WriteSubDocument()` - **TokenSink provides exactly this functionality**.

## Token Streaming API Capabilities

### TokenSource: Streaming Tokenization

**What it provides**:
- ✅ Streaming tokenization from `io.Reader` (not requiring full document in memory)
- ✅ Maintains state across reads (`tkState`, `PosDoc`)
- ✅ Handles context-dependent tokenization (multiline strings, comments)
- ✅ Buffer management with compaction
- ✅ EOF handling with trailing newline support

**Storage System Usage**:
```go
// Read sub-document from offset (as specified in inverted_index_subdocuments.md)
func readSubDocumentFromOffset(reader io.ReaderAt, snapshotOffset int64, ref SubDocRef) (*ir.Node, error) {
    absoluteOffset := snapshotOffset + snapshotEntryHeaderSize + ref.Offset
    sectionReader := io.NewSectionReader(reader, absoluteOffset, ref.Size)
    
    // TokenSource provides exactly this capability
    source := token.NewTokenSource(sectionReader)
    
    // Parse directly from TokenSource (streaming, no token collection)
    return parse.ParseFromTokenSource(source)
}
```

**Impact**: ✅ **Directly enables** streaming sub-document reads without loading entire snapshots.

### TokenSink: Streaming Encoding with Offset Tracking

**What it provides**:
- ✅ Streaming token encoding to `io.Writer`
- ✅ **Absolute byte offset tracking** (`Offset()` method)
- ✅ **Node start detection** with callback (`onNodeStart`)
- ✅ **Path tracking** (nested structures, arrays, sparse arrays)
- ✅ Proper formatting (spacing, newlines)

**Storage System Usage**:
```go
// Write snapshot with sub-document indexing (as specified in streaming_writer_design.md)
func writeSnapshotWithSubDocIndexing(writer io.Writer, index *InvertedIndex) error {
    var nodeStarts []struct {
        offset int64
        path   string
    }
    
    // TokenSink provides exactly this capability
    sink := token.NewTokenSink(writer, func(offset int, path string, token token.Token) {
        // Record node start for inverted index
        nodeStarts = append(nodeStarts, struct {
            offset int64
            path   string
        }{int64(offset), path})
    })
    
    // Write nodes using existing encode logic
    for _, node := range nodes {
        tokens := tokenizeNode(node) // Existing tokenization
        sink.Write(tokens) // TokenSink tracks offsets and calls callback
    }
    
    // Build inverted index from recorded offsets
    for _, ns := range nodeStarts {
        index.PathToSubDocs[ns.path] = append(
            index.PathToSubDocs[ns.path],
            SubDocRef{Offset: ns.offset, ...})
    }
    
    return nil
}
```

**Impact**: ✅ **Directly enables** position-aware writing with offset tracking for inverted index.

## Impact Analysis

### 1. Enables Inverted Index Sub-Document Approach

**Before Token Streaming**:
- ❌ Would need to load entire snapshot into memory to tokenize
- ❌ Cannot process incrementally
- ❌ Memory constraints prevent handling large snapshots (1GB+)

**With Token Streaming**:
- ✅ Can tokenize from stream (`TokenSource`)
- ✅ Can process incrementally without full memory load
- ✅ Can handle arbitrarily large snapshots

**Impact**: **CRITICAL** - Without streaming, the inverted index sub-document approach is not feasible for large snapshots.

### 2. Enables Snapshot Path Indexing

**Before Token Streaming**:
- ❌ Cannot seek to specific paths within snapshots
- ❌ Must read entire snapshot to find paths
- ❌ No way to track offsets during encoding

**With Token Streaming**:
- ✅ Can tokenize from arbitrary offsets (`TokenSource` with `io.SectionReader`)
- ✅ Can track offsets during encoding (`TokenSink.Offset()`)
- ✅ Can detect node starts for path indexing (`TokenSink` callback)

**Impact**: **CRITICAL** - Enables 1000x I/O reduction for controller use case (10^6:1 ratio).

### 3. Memory Efficiency

**Before Token Streaming**:
- ❌ Must load entire document into memory for tokenization
- ❌ Memory usage scales with document size
- ❌ Cannot handle documents larger than available memory

**With Token Streaming**:
- ✅ Processes documents incrementally
- ✅ Memory usage bounded by buffer size (default 4KB)
- ✅ Can handle documents larger than available memory

**Impact**: **HIGH** - Essential for handling large snapshots without memory constraints.

### 4. Enables Streaming Writer Design

**Before Token Streaming**:
- ❌ No way to track byte offsets during encoding
- ❌ Cannot detect node boundaries during encoding
- ❌ Must encode entire document to know offsets

**With Token Streaming**:
- ✅ `TokenSink` tracks absolute byte offsets
- ✅ `TokenSink` detects node starts via callback
- ✅ Can record offsets as encoding progresses

**Impact**: **CRITICAL** - Enables the position-aware writer design specified in `streaming_writer_design.md`.

### 5. Enables Partial Document Reads

**Before Token Streaming**:
- ❌ Must read entire document to parse
- ❌ Cannot read sub-documents from offsets
- ❌ No way to seek within documents

**With Token Streaming**:
- ✅ Can read from arbitrary offsets using `io.SectionReader`
- ✅ `TokenSource` tokenizes from stream
- ✅ Can parse partial documents (sub-documents)

**Impact**: **CRITICAL** - Enables reading only relevant sub-documents instead of entire snapshots.

## Alignment with Design Documents

### ✅ Matches Streaming Parse Design (`streaming_parse_design.md`)

**Design Document Says**:
> "Tokenize from stream, parse from tokens (streaming parse, not loading all)"

**Token Streaming Provides**:
- ✅ `TokenSource` tokenizes from `io.Reader` (streaming)
- ✅ Returns tokens incrementally (not requiring full document)
- ✅ Can be used with `io.SectionReader` for partial reads

**Status**: ✅ **Fully aligned** - TokenSource provides exactly what the design document specifies.

### ✅ Matches Streaming Writer Design (`streaming_writer_design.md`)

**Design Document Says**:
> "Position-aware writer that tracks byte positions and detects node starts"

**Token Streaming Provides**:
- ✅ `TokenSink` tracks absolute byte offsets (`Offset()`)
- ✅ `TokenSink` detects node starts via callback (`onNodeStart`)
- ✅ `TokenSink` tracks paths during encoding

**Status**: ✅ **Fully aligned** - TokenSink provides exactly what the design document specifies.

### ✅ Matches Inverted Index Requirements (`inverted_index_subdocuments.md`)

**Design Document Says**:
> "Tokenization from stream - parse wire format from stream, not memory buffer"

**Token Streaming Provides**:
- ✅ `TokenSource` tokenizes from stream
- ✅ No requirement to load entire document
- ✅ Can handle arbitrarily large documents

**Status**: ✅ **Fully aligned** - TokenSource enables the streaming requirements.

### ✅ Matches Path Indexing Requirements (`snapshot_path_indexing.md`)

**Design Document Says**:
> "Tokenize wire format, count brackets to find document boundaries, track offsets"

**Token Streaming Provides**:
- ✅ `TokenSource` tokenizes wire format from stream
- ✅ Tokens include bracket types (`TLCurl`, `TRCurl`, `TLSquare`, `TRSquare`)
- ✅ `TokenSink` tracks offsets during encoding

**Status**: ✅ **Fully aligned** - Token streaming provides the foundation for path indexing.

## Gaps and Considerations

### 1. Integration with Existing Encode Package

**Current State**: Storage system uses `encode.Encode()` for serialization.

**Consideration**: TokenSink works with tokens, not `*ir.Node`. Need to:
- Either: Tokenize nodes before writing to TokenSink
- Or: Integrate TokenSink into encode package

**Recommendation**: Tokenize nodes using existing `Tokenize()` function, then write tokens to `TokenSink`. This maintains separation of concerns.

### 2. Parsing from Tokens

**Current State**: TokenSource provides tokens, but parsing logic may need updates.

**Consideration**: Need to ensure parsing logic can work with tokens from TokenSource (which may have different characteristics than tokens from `Tokenize()`).

**Recommendation**: Verify that existing parsing logic works with TokenSource tokens. TokenSource should produce identical tokens to `Tokenize()` for same input.

### 3. Wire Format vs Indented Format

**Current State**: Design documents recommend wire format for snapshots.

**Consideration**: TokenSource/TokenSink work with any format, but wire format is recommended for:
- Compact representation
- Easier boundary detection
- Better seeking support

**Recommendation**: ✅ Use wire format for snapshots (as recommended in design docs). Token streaming works with any format.

### 4. Buffer Size Configuration

**Current State**: TokenSource uses default 4KB buffer.

**Consideration**: For very large multiline strings or complex documents, may need larger buffer.

**Recommendation**: Make buffer size configurable if needed. Default 4KB should be sufficient for most cases.

## Implementation Impact

### Required Changes to Storage System

#### 1. Sub-Document Reading

**Current**: (Not implemented - design phase)

**With Token Streaming**:
```go
func readSubDocument(reader io.ReaderAt, offset int64, size int64) (*ir.Node, error) {
    sectionReader := io.NewSectionReader(reader, offset, size)
    source := token.NewTokenSource(sectionReader)
    
    // Parse directly from TokenSource (streaming, no token collection)
    return parse.ParseFromTokenSource(source)
}
```

**Impact**: ✅ **Enables** sub-document reads without loading entire snapshot.

#### 2. Snapshot Writing with Offset Tracking

**Current**: Uses `encode.Encode()` directly (no offset tracking)

**With Token Streaming**:
```go
func writeSnapshotWithOffsets(writer io.Writer, snapshot *ir.Node) (map[string]int64, error) {
    pathOffsets := make(map[string]int64)
    
    // Tokenize node
    tokens, err := token.Tokenize(nil, encodeToBytes(snapshot))
    if err != nil {
        return nil, err
    }
    
    // Write with offset tracking
    sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
        pathOffsets[path] = int64(offset)
    })
    
    if err := sink.Write(tokens); err != nil {
        return nil, err
    }
    
    return pathOffsets, nil
}
```

**Impact**: ✅ **Enables** offset tracking for path indexing.

#### 3. Inverted Index Building

**Current**: (Not implemented - design phase)

**With Token Streaming**:
```go
func buildInvertedIndex(writer io.Writer, nodes map[string]*ir.Node) (*InvertedIndex, error) {
    index := &InvertedIndex{
        PathToSubDocs: make(map[string][]SubDocRef),
    }
    
    sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
        // Record sub-document start
        ref := SubDocRef{
            Offset: int64(offset),
            // ... other fields
        }
        index.PathToSubDocs[path] = append(index.PathToSubDocs[path], ref)
    })
    
    // Write nodes with offset tracking
    for path, node := range nodes {
        tokens := tokenizeNode(node)
        sink.Write(tokens)
    }
    
    return index, nil
}
```

**Impact**: ✅ **Enables** building inverted index during snapshot write.

## Performance Impact

### Memory Usage

**Before**: O(document_size) - Must load entire document
**After**: O(buffer_size) - Bounded by buffer size (default 4KB)

**Improvement**: **1000x+ reduction** for large documents (1GB snapshot → 4KB buffer)

### I/O Efficiency

**Before**: Must read entire snapshot to find paths
**After**: Can read only relevant sub-documents

**Improvement**: **1000x reduction** for controller use case (1GB snapshot → 1KB sub-doc)

### CPU Efficiency

**Before**: Tokenize entire document upfront
**After**: Tokenize incrementally as needed

**Improvement**: **Similar** - But enables processing documents that wouldn't fit in memory

## Risk Assessment

### Low Risk ✅

- **API Stability**: TokenSource/TokenSink have clean, stable APIs
- **Compatibility**: Works with existing `Tokenize()` function
- **Testing**: Comprehensive test coverage in source/sink tests

### Medium Risk ⚠️

- **Integration**: Need to verify integration with existing encode/parse logic
- **Edge Cases**: Need to test with very large documents and edge cases
- **Performance**: Need to measure actual performance impact

### Mitigation

1. ✅ **Comprehensive Tests**: Token streaming has extensive test coverage
2. ✅ **Incremental Integration**: Can integrate gradually, test at each step
3. ✅ **Fallback**: Can fall back to non-streaming approach if needed

## Conclusion

### Impact Summary

The token streaming API (`TokenSource` and `TokenSink`) has **CRITICAL** impact on the storage redesign:

1. ✅ **Enables inverted index sub-document approach** - Essential for handling large snapshots
2. ✅ **Enables snapshot path indexing** - Critical for 1000x I/O reduction
3. ✅ **Enables memory-efficient processing** - Can handle documents larger than memory
4. ✅ **Enables position-aware writing** - Required for offset tracking
5. ✅ **Enables partial document reads** - Essential for reading only relevant sub-documents

### Alignment Status

✅ **Fully Aligned** - Token streaming API directly addresses all requirements identified in design documents:
- `streaming_parse_design.md` ✅
- `streaming_writer_design.md` ✅
- `inverted_index_subdocuments.md` ✅
- `snapshot_path_indexing.md` ✅

### Recommendation

**✅ PROCEED** - Token streaming API is essential for the storage redesign. It provides exactly what the design documents specify and enables critical optimizations.

**Next Steps**:
1. ✅ Verify integration with existing encode/parse logic
2. ✅ Implement sub-document reading using TokenSource
3. ✅ Implement snapshot writing with offset tracking using TokenSink
4. ✅ Build inverted index using TokenSink callbacks
5. ✅ Measure performance impact

**Priority**: **HIGH** - Token streaming is a foundational capability for the storage redesign.
