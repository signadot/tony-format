# Streaming Writer Design: Position-Aware Writer for Inverted Index

## The Problem

We need a streaming writer that:
1. **Tracks byte positions** as it writes (for precise offset tracking)
2. **Identifies sub-document boundaries** as it writes
3. **Records precise offsets** in the inverted index (not approximate offsets)

**Key Constraint**: The inverted index must store **precise document-boundary offsets**, not approximate offsets. We need to know exactly where each sub-document starts and ends.

## Current Architecture

**Current Flow**:
```
ir.Node → encode.Encode(node, writer) → writes to io.Writer
```

**Key Characteristics**:
- `encode.Encode` takes `*ir.Node` and `io.Writer`
- Writes directly to writer (no position tracking)
- No way to know where sub-documents start/end
- No way to record boundaries in inverted index

## Goal: Add Position-Aware Streaming Writer

**Requirements**:
1. **Track byte positions** - know exactly where we are in the output stream
2. **Identify sub-document boundaries** - detect when a complete sub-document has been written
3. **Record precise offsets** - store exact start/end positions in inverted index
4. **Don't break existing API** - keep `encode.Encode` working
5. **Reuse existing logic** - leverage `encode.Encode` for actual writing

## Design Approach

### Option A: Position-Tracking Writer Wrapper

**Flow**:
```
PositionWriter wraps io.Writer
  → Tracks current offset (bytes written)
  → Wraps encode.Encode calls
  → Records boundaries when sub-documents complete
```

**Implementation**:
```go
// PositionWriter wraps an io.Writer and tracks byte positions
type PositionWriter struct {
    writer io.Writer
    offset int64  // Current byte offset
    index  *InvertedIndex  // Index to update
}

func (pw *PositionWriter) Write(p []byte) (int, error) {
    n, err := pw.writer.Write(p)
    pw.offset += int64(n)
    return n, err
}

func (pw *PositionWriter) Offset() int64 {
    return pw.offset
}

// WriteSubDocument writes a sub-document and records its boundaries
func (pw *PositionWriter) WriteSubDocument(node *ir.Node, kindedPath string, subDocID SubDocID) error {
    startOffset := pw.offset
    
    // Write the node using existing encode logic
    err := encode.Encode(node, pw, encode.EncodeWire(true)) // Use wire format
    if err != nil {
        return err
    }
    
    endOffset := pw.offset
    size := endOffset - startOffset
    
    // Record in inverted index
    ref := SubDocRef{
        SubDocID: subDocID,
        Offset:   startOffset,
        Size:     size,
    }
    pw.index.PathToSubDocs[kindedPath] = append(
        pw.index.PathToSubDocs[kindedPath], ref)
    
    pw.index.SubDocs[subDocID] = SubDocMeta{
        SubDocOffset: startOffset,
        SubDocSize:   size,
        PathPrefix:   kindedPath,
    }
    
    return nil
}
```

**Pros**:
- ✅ **Simple** - wraps existing writer, tracks offset
- ✅ **Reuses existing code** - uses `encode.Encode` as-is
- ✅ **Precise offsets** - knows exact start/end positions
- ✅ **No changes to encode** - works with existing API

**Cons**:
- ⚠️ Must call `WriteSubDocument` explicitly (not automatic)
- ⚠️ Doesn't automatically detect boundaries

### Option B: Streaming Writer with Boundary Detection

**Flow**:
```
StreamingWriter wraps io.Writer
  → Tracks current offset
  → Wraps encode.Encode calls
  → Detects when sub-document boundaries are reached
  → Automatically records boundaries
```

**Implementation**:
```go
// StreamingWriter tracks positions and detects boundaries
type StreamingWriter struct {
    writer io.Writer
    offset int64
    index  *InvertedIndex
    
    // Current sub-document tracking
    currentSubDocStart int64
    currentSubDocPath  string
    currentSubDocID    SubDocID
}

func (sw *StreamingWriter) Write(p []byte) (int, error) {
    n, err := sw.writer.Write(p)
    sw.offset += int64(n)
    return n, err
}

func (sw *StreamingWriter) Offset() int64 {
    return sw.offset
}

// StartSubDocument marks the start of a new sub-document
func (sw *StreamingWriter) StartSubDocument(kindedPath string, subDocID SubDocID) {
    sw.currentSubDocStart = sw.offset
    sw.currentSubDocPath = kindedPath
    sw.currentSubDocID = subDocID
}

// EndSubDocument marks the end of a sub-document and records it
func (sw *StreamingWriter) EndSubDocument() {
    if sw.currentSubDocPath == "" {
        return // No active sub-document
    }
    
    endOffset := sw.offset
    size := endOffset - sw.currentSubDocStart
    
    // Record in inverted index
    ref := SubDocRef{
        SubDocID: sw.currentSubDocID,
        Offset:   sw.currentSubDocStart,
        Size:     size,
    }
    sw.index.PathToSubDocs[sw.currentSubDocPath] = append(
        sw.index.PathToSubDocs[sw.currentSubDocPath], ref)
    
    sw.index.SubDocs[sw.currentSubDocID] = SubDocMeta{
        SubDocOffset: sw.currentSubDocStart,
        SubDocSize:   size,
        PathPrefix:   sw.currentSubDocPath,
    }
    
    // Reset
    sw.currentSubDocPath = ""
}

// WriteSubDocument writes a complete sub-document
func (sw *StreamingWriter) WriteSubDocument(node *ir.Node, kindedPath string, subDocID SubDocID) error {
    sw.StartSubDocument(kindedPath, subDocID)
    
    // Write the node
    err := encode.Encode(node, sw, encode.EncodeWire(true))
    if err != nil {
        return err
    }
    
    sw.EndSubDocument()
    return nil
}
```

**Pros**:
- ✅ **Explicit boundary control** - caller controls when sub-docs start/end
- ✅ **Precise offsets** - tracks exact positions
- ✅ **Reuses encode** - uses existing serialization
- ✅ **Flexible** - can write multiple sub-docs in sequence

**Cons**:
- ⚠️ Requires explicit `StartSubDocument`/`EndSubDocument` calls
- ⚠️ Caller must manage sub-document boundaries

### Option C: Automatic Boundary Detection (Complex)

**Flow**:
```
StreamingWriter wraps io.Writer
  → Tracks current offset
  → Intercepts encode.Encode calls
  → Analyzes structure to detect boundaries
  → Automatically records boundaries
```

**Pros**:
- ✅ Automatic boundary detection
- ✅ No manual boundary management

**Cons**:
- ❌ **Very complex** - must analyze structure during encoding
- ❌ **Hard to get right** - boundary detection is ambiguous
- ❌ **Tight coupling** - must understand encode internals
- ❌ **Not recommended** - too complex for our use case

## Recommendation: Option B (Streaming Writer with Explicit Boundaries)

**Rationale**:
1. **Explicit is better than implicit** - caller knows when sub-docs start/end
2. **Precise offsets** - tracks exact byte positions
3. **Reuses existing code** - uses `encode.Encode` as-is
4. **Simple** - just wraps writer and tracks offset
5. **Flexible** - can write multiple sub-docs, handle edge cases

**Implementation**:
```go
package storage

import (
    "io"
    "github.com/signadot/tony-format/go-tony/encode"
    "github.com/signadot/tony-format/go-tony/ir"
)

// StreamingWriter tracks byte positions and records sub-document boundaries
type StreamingWriter struct {
    writer io.Writer
    offset int64
    
    // Current sub-document tracking
    currentSubDocStart int64
    currentSubDocPath  string
    currentSubDocID    SubDocID
    
    // Inverted index to update
    index *InvertedIndex
}

// NewStreamingWriter creates a new position-aware writer
func NewStreamingWriter(writer io.Writer, index *InvertedIndex) *StreamingWriter {
    return &StreamingWriter{
        writer: writer,
        offset: 0,
        index:  index,
    }
}

// Write implements io.Writer, tracking bytes written
func (sw *StreamingWriter) Write(p []byte) (int, error) {
    n, err := sw.writer.Write(p)
    sw.offset += int64(n)
    return n, err
}

// Offset returns the current byte offset
func (sw *StreamingWriter) Offset() int64 {
    return sw.offset
}

// StartSubDocument marks the start of a new sub-document
func (sw *StreamingWriter) StartSubDocument(kindedPath string, subDocID SubDocID) {
    sw.currentSubDocStart = sw.offset
    sw.currentSubDocPath = kindedPath
    sw.currentSubDocID = subDocID
}

// EndSubDocument marks the end of a sub-document and records it in the index
func (sw *StreamingWriter) EndSubDocument() error {
    if sw.currentSubDocPath == "" {
        return nil // No active sub-document
    }
    
    endOffset := sw.offset
    size := endOffset - sw.currentSubDocStart
    
    // Record in inverted index
    ref := SubDocRef{
        SubDocID: sw.currentSubDocID,
        Offset:   sw.currentSubDocStart,
        Size:     size,
    }
    
    if sw.index.PathToSubDocs == nil {
        sw.index.PathToSubDocs = make(map[string][]SubDocRef)
    }
    sw.index.PathToSubDocs[sw.currentSubDocPath] = append(
        sw.index.PathToSubDocs[sw.currentSubDocPath], ref)
    
    if sw.index.SubDocs == nil {
        sw.index.SubDocs = make(map[SubDocID]SubDocMeta)
    }
    sw.index.SubDocs[sw.currentSubDocID] = SubDocMeta{
        SubDocOffset: sw.currentSubDocStart,
        SubDocSize:   size,
        PathPrefix:   sw.currentSubDocPath,
    }
    
    // Reset
    sw.currentSubDocPath = ""
    return nil
}

// WriteSubDocument writes a complete sub-document and records its boundaries
func (sw *StreamingWriter) WriteSubDocument(node *ir.Node, kindedPath string, subDocID SubDocID, opts ...encode.EncodeOption) error {
    sw.StartSubDocument(kindedPath, subDocID)
    
    // Write the node using existing encode logic
    // Use wire format for compact, bracket-delimited output
    encodeOpts := append([]encode.EncodeOption{encode.EncodeWire(true)}, opts...)
    err := encode.Encode(node, sw, encodeOpts...)
    if err != nil {
        return err
    }
    
    return sw.EndSubDocument()
}
```

## Usage for Snapshot Writing

```go
// Write snapshot with sub-document indexing
func writeSnapshotWithSubDocIndexing(writer io.Writer, reader io.ReaderAt, 
                                     diffs []LogSegment, commit int64, 
                                     maxSubDocSize int64) (*InvertedIndex, error) {
    index := &InvertedIndex{
        PathToSubDocs: make(map[string][]SubDocRef),
        SubDocs: make(map[SubDocID]SubDocMeta),
    }
    
    sw := NewStreamingWriter(writer, index)
    subDocID := SubDocID(0)
    currentState := make(map[string]*ir.Node)
    
    // Read and apply diffs incrementally
    for _, diffSeg := range diffs {
        // Read diff from log
        diff := readDiff(reader, diffSeg.LogPosition)
        
        // Apply diff to current state
        applyDiffToState(currentState, diff, diffSeg.KindedPath)
        
        // Check if we've reached a sub-document boundary
        if shouldCreateSubDoc(currentState, diffSeg.KindedPath, maxSubDocSize) {
            pathValue := currentState[diffSeg.KindedPath]
            
            // Write sub-document with precise offset tracking
            err := sw.WriteSubDocument(pathValue, diffSeg.KindedPath, subDocID)
            if err != nil {
                return nil, err
            }
            
            subDocID++
            
            // Clear current state for this path (we've written it)
            delete(currentState, diffSeg.KindedPath)
        }
    }
    
    // Write any remaining state
    for kindedPath, pathValue := range currentState {
        err := sw.WriteSubDocument(pathValue, kindedPath, subDocID)
        if err != nil {
            return nil, err
        }
        subDocID++
    }
    
    return index, nil
}
```

## Key Features

1. **Position Tracking**: `Offset()` returns current byte position
2. **Boundary Recording**: `WriteSubDocument` records precise start/end offsets
3. **Wire Format**: Uses wire format (brackets) for compact, parseable output
4. **Inverted Index**: Automatically updates inverted index with precise offsets
5. **Reuses Encode**: Uses existing `encode.Encode` logic (no rewrite needed)

## Benefits

- ✅ **Precise offsets** - knows exactly where sub-documents start/end
- ✅ **Simple** - wraps writer, tracks offset, records boundaries
- ✅ **Reuses existing code** - uses `encode.Encode` as-is
- ✅ **No changes to encode** - works with existing API
- ✅ **Flexible** - can write multiple sub-docs, handle edge cases
- ✅ **Wire format** - compact, bracket-delimited output (easy to parse)

## Implementation Considerations

### Wire Format vs. Indented Format

**Wire Format** (recommended):
- Uses brackets `{` `}` and `[` `]` for structure
- Compact, no indentation
- Easy to detect boundaries (bracket counting)
- Fast to parse

**Indented Format**:
- Uses indentation for structure
- More readable
- Harder to detect boundaries (requires tokenization)
- Slower to parse

**Recommendation**: Use **wire format** for snapshots (compact, fast, easy boundaries).

### Offset Tracking

**Current Offset**: Tracks bytes written via `Write()` calls
- Simple: increment offset by bytes written
- Accurate: reflects actual bytes in output stream
- Works with any writer (file, buffer, etc.)

### Boundary Detection

**Explicit Boundaries** (recommended):
- Caller calls `StartSubDocument` / `EndSubDocument`
- Caller knows when sub-docs start/end
- Precise control over boundaries

**Automatic Boundaries** (not recommended):
- Would require analyzing structure during encoding
- Complex, error-prone, tight coupling

## Comparison to Streaming Reader

**Streaming Reader**:
- Reads from `io.ReaderAt` at precise offsets
- Uses `FindDocumentBoundary` to find exact boundaries
- Parses sub-documents using `ParseFromReader`

**Streaming Writer**:
- Writes to `io.Writer` with position tracking
- Records precise offsets when sub-documents are written
- Uses `encode.Encode` to serialize sub-documents

**Symmetry**:
- Reader: offset → find boundary → parse
- Writer: write → record boundary → store offset
- Both use precise offsets for inverted index

## Next Steps

1. **Implement `StreamingWriter`**:
   - Position tracking (`Write`, `Offset`)
   - Boundary recording (`StartSubDocument`, `EndSubDocument`, `WriteSubDocument`)
   - Inverted index updates

2. **Integrate with snapshot writing**:
   - Use `StreamingWriter` in `writeSnapshotWithSubDocIndexing`
   - Record precise offsets for each sub-document
   - Store offsets in inverted index

3. **Test with real snapshots**:
   - Write snapshots with sub-document indexing
   - Verify precise offsets are recorded
   - Read back using streaming reader

4. **Measure performance**:
   - Compare wire format vs. indented format
   - Measure index size vs. snapshot size
   - Tune size thresholds

## Conclusion

**Recommendation**: **Option B (Streaming Writer with Explicit Boundaries)**

**Implementation**:
- `StreamingWriter` wraps `io.Writer`, tracks offset
- `WriteSubDocument` writes node and records precise boundaries
- Uses existing `encode.Encode` logic (wire format)
- Updates inverted index with exact offsets

**Benefits**:
- ✅ Precise offsets (exact byte positions)
- ✅ Simple (wraps writer, tracks offset)
- ✅ Reuses existing code (`encode.Encode`)
- ✅ Flexible (explicit boundary control)
- ✅ Works with inverted index (precise offsets)
