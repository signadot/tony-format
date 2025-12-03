# Inverted Index with Sub-Document Indexing

## The Problem

Our use case is fundamentally different from typical databases:
- **Typical DBs**: Many separate documents (e.g., MongoDB has many user documents)
- **Our case**: ONE giant virtual evolving document (the whole database state)
- **Reading "full document"**: Would mean reading entire database state (not feasible)

**Key Insight**: We need to break the giant document into indexable sub-documents and use inverted indexes to find which sub-documents contain a path.

## Proposed Approach: Inverted Index + Sub-Documents

### Concept

1. **Break snapshot into sub-documents** (chunks)
2. **Index each sub-document** (inverted index: path → sub-document IDs)
3. **Read only relevant sub-documents** (not entire snapshot)

**Example**:
```
Giant snapshot (1GB):
  Sub-doc 1: resources.resource1 (1KB)
  Sub-doc 2: resources.resource2 (1KB)
  ...
  Sub-doc 10000: resources.resource10000 (1KB)
  
Inverted index:
  "resources.resource1" → [sub-doc-1]
  "resources.resource2" → [sub-doc-2]
  ...
  
Read "resources.resource1":
  Look up in inverted index → sub-doc-1
  Read only sub-doc-1 (1KB) instead of entire snapshot (1GB)
```

## Design

### Sub-Document Structure

**Question**: How do we break the giant document into sub-documents?

**Option A: By Path Depth**
- Each path segment becomes a sub-document boundary
- Example: `resources.resource1` → sub-document at `resources.resource1`
- Natural boundaries at path segments

**Option B: By Size**
- Break into fixed-size chunks (e.g., 64KB chunks)
- Each chunk is a sub-document
- Less natural, but size-controlled

**Option C: By Path + Size Hybrid**
- Prefer path boundaries (natural)
- But limit sub-document size (e.g., max 1MB per sub-doc)
- Split large paths into multiple sub-docs if needed

**Recommendation**: **Option C (Hybrid)** - Natural path boundaries with size limits.

### Inverted Index Structure

**Index Maps**: Path → Sub-document IDs (and offsets)

```go
type InvertedIndex struct {
    // Maps path → list of sub-documents containing that path
    PathToSubDocs map[string][]SubDocRef
    
    // Sub-document metadata
    SubDocs map[SubDocID]SubDocMeta
}

type SubDocRef struct {
    SubDocID SubDocID
    Offset   int64  // Offset within snapshot where sub-doc starts
    Size     int64  // Size of sub-document
}

type SubDocMeta struct {
    SnapshotCommit int64
    SnapshotOffset int64  // Offset of snapshot entry in log file
    SubDocOffset   int64  // Offset within snapshot where sub-doc starts
    SubDocSize     int64  // Size of sub-document
    PathPrefix     string // Path prefix for this sub-doc (e.g., "resources.resource1")
}
```

### Size Threshold

**Question**: Which sub-documents should be indexed?

**Approach**: Index sub-documents up to a certain size threshold.

**Rationale**:
- Small sub-documents: Indexing overhead is worth it (large I/O savings)
- Large sub-documents: Indexing overhead might not be worth it (less I/O savings)
- Threshold: e.g., 1MB - index sub-docs < 1MB, full read for larger ones

**Example**:
```
Sub-doc 1: resources.resource1 (1KB) → Index it
Sub-doc 2: resources.resource2 (1KB) → Index it
...
Sub-doc 10000: resources.resource10000 (1KB) → Index it
Sub-doc 10001: resources.largeResource (10MB) → Don't index, read full if needed
```

## Implementation

**Key Constraint**: Cannot load entire snapshot/document into memory. Must process incrementally/streaming.

### Snapshot Write with Sub-Document Indexing

**Key Constraint**: Cannot load entire snapshot into memory. Must stream/process incrementally.

```go
// Write snapshot by reading diffs and applying them incrementally
func writeSnapshotWithSubDocIndexing(writer io.Writer, reader io.ReaderAt, 
                                     diffs []LogSegment, commit int64, 
                                     maxSubDocSize int64) (*InvertedIndex, error) {
    snapshotStart := writer.Position()
    index := &InvertedIndex{
        PathToSubDocs: make(map[string][]SubDocRef),
        SubDocs: make(map[SubDocID]SubDocMeta),
    }
    
    subDocID := SubDocID(0)
    currentState := make(map[string]*ir.Node) // Only current path being processed
    
    // Read and apply diffs incrementally
    for _, diffSeg := range diffs {
        // Read diff from log
        diff := readDiff(reader, diffSeg.LogPosition)
        
        // Apply diff to current state (streaming, not loading all)
        applyDiffToState(currentState, diff, diffSeg.KindedPath)
        
        // Check if we've reached a sub-document boundary
        if shouldCreateSubDoc(currentState, diffSeg.KindedPath, maxSubDocSize) {
            subDocStart := writer.Position() - snapshotStart
            
            // Write sub-document (only current path, not entire state)
            size := writeSubDocumentFromState(writer, currentState, diffSeg.KindedPath)
            
            // Index this sub-document
            ref := SubDocRef{
                SubDocID: subDocID,
                Offset:   subDocStart,
                Size:     size,
            }
            index.PathToSubDocs[diffSeg.KindedPath] = append(
                index.PathToSubDocs[diffSeg.KindedPath], ref)
            
            index.SubDocs[subDocID] = SubDocMeta{
                SnapshotCommit: commit,
                SubDocOffset:   subDocStart,
                SubDocSize:     size,
                PathPrefix:     diffSeg.KindedPath,
            }
            
            subDocID++
            
            // Clear current state for this path (we've written it)
            delete(currentState, diffSeg.KindedPath)
        }
    }
    
    return index, nil
}

// Apply diff to state incrementally (streaming)
func applyDiffToState(state map[string]*ir.Node, diff *ir.Node, kindedPath string) {
    // Extract path from diff and merge into state
    // Only keep state for paths we're currently processing
    // Don't load entire document into memory
    pathValue := extractPathFromDiff(diff, kindedPath)
    state[kindedPath] = pathValue
}

// Write sub-document from state (only the specific path)
func writeSubDocumentFromState(writer io.Writer, state map[string]*ir.Node, 
                               kindedPath string) int64 {
    // Write only this path's value, not entire state
    pathValue := state[kindedPath]
    startPos := writer.Position()
    
    // Write using wire format, tokenizing as we go
    writeNodeWireFormat(writer, pathValue)
    
    return writer.Position() - startPos
}

func shouldCreateSubDoc(state map[string]*ir.Node, kindedPath string, maxSize int64) bool {
    // Create sub-doc if:
    // 1. Path is at a natural boundary (e.g., "resources.resource1")
    // 2. Estimated size is within threshold (< maxSize)
    
    pathValue := state[kindedPath]
    estimatedSize := estimateNodeSize(pathValue) // Estimate without fully serializing
    
    return isNaturalBoundary(kindedPath) && estimatedSize < maxSize
}
```

**Key Changes**:
- ✅ **No `snapshot *ir.Node` parameter** - don't load entire snapshot
- ✅ **Stream processing** - read diffs incrementally, apply to state
- ✅ **State management** - only keep current paths being processed
- ✅ **Incremental writing** - write sub-docs as we process, not all at once

### Read with Inverted Index

**Key Constraint**: Cannot load entire snapshot into memory. Must read only needed sub-documents.

```go
func readPathWithInvertedIndex(kindedPath string, commit int64, 
                               reader io.ReaderAt) (*ir.Node, error) {
    // Find snapshot ≤ commit (just metadata, not full content)
    snapshotMeta := findSnapshotMeta(commit)
    
    // Look up path in inverted index
    subDocRefs := snapshotMeta.InvertedIndex.PathToSubDocs[kindedPath]
    
    if len(subDocRefs) == 0 {
        // Path not indexed (too large or not found)
        // Fall back to reading larger chunk or applying diffs
        return readPathFromDiffs(kindedPath, commit, reader)
    }
    
    // Read only relevant sub-documents (streaming, not loading all)
    var result *ir.Node
    for _, ref := range subDocRefs {
        // Read sub-document from offset (streaming read)
        subDoc := readSubDocumentFromOffset(reader, snapshotMeta.LogPosition, ref)
        result = mergeSubDoc(result, subDoc)
    }
    
    return result, nil
}

func readSubDocumentFromOffset(reader io.ReaderAt, snapshotOffset int64, 
                                ref SubDocRef) (*ir.Node, error) {
    // Calculate absolute offset: snapshot entry start + entry header + sub-doc offset
    absoluteOffset := snapshotOffset + snapshotEntryHeaderSize + ref.Offset
    
    // Seek to sub-document offset
    sectionReader := io.NewSectionReader(reader, absoluteOffset, ref.Size)
    
    // Read sub-document using wire format + tokenization (streaming)
    // Tokenize from offset, count brackets to find boundaries
    subDoc := readSubDocFromStream(sectionReader, ref.Size)
    
    return subDoc, nil
}

// Read sub-document from stream (doesn't load entire snapshot)
func readSubDocFromStream(reader io.Reader, size int64) (*ir.Node, error) {
    // Tokenize wire format from stream
    tokens := tokenizeWireFormatStream(reader)
    
    // Parse from tokens (streaming parse, not loading all)
    node := parseNodeFromTokens(tokens)
    
    return node, nil
}

// Fallback: Read path by applying diffs (if not indexed)
func readPathFromDiffs(kindedPath string, commit int64, reader io.ReaderAt) (*ir.Node, error) {
    // Find all diffs up to commit
    diffs := findDiffsUpToCommit(kindedPath, commit)
    
    // Apply diffs incrementally (streaming)
    var state *ir.Node
    for _, diffSeg := range diffs {
        diff := readDiff(reader, diffSeg.LogPosition)
        state = applyDiff(state, diff, kindedPath)
    }
    
    return state, nil
}
```

**Key Changes**:
- ✅ **No loading entire snapshot** - only read metadata (offsets, sizes)
- ✅ **Streaming reads** - read sub-docs from offsets, not entire snapshot
- ✅ **Section readers** - use `io.SectionReader` to read only needed bytes
- ✅ **Tokenization from stream** - parse from stream, not from memory
- ✅ **Fallback strategy** - if not indexed, apply diffs incrementally

## Comparison to Previous Approaches

### Previous: Path Indexing at Byte Offsets

**Approach**: Index every path → byte offset within snapshot

**Pros**:
- Precise: Can read exact path
- Simple: One offset per path

**Cons**:
- **Index size**: 10,000 paths → 10,000 offsets (large index)
- **No grouping**: Each path indexed separately
- **Redundancy**: Similar paths indexed separately

### New: Inverted Index + Sub-Documents

**Approach**: Break into sub-documents, index sub-documents

**Pros**:
- **Grouping**: Related paths in same sub-doc
- **Smaller index**: Fewer sub-docs than individual paths
- **Natural boundaries**: Sub-docs at natural path boundaries
- **Size control**: Can limit sub-doc size

**Cons**:
- More complex: Must break into sub-docs
- Might read slightly more: Entire sub-doc, not just path
- But: Sub-docs are small (1KB), so still efficient

## Size Threshold Strategy

### Indexing Threshold

**Recommendation**: Index sub-documents up to 1MB

**Rationale**:
- Small sub-docs (< 1MB): Indexing overhead worth it
- Large sub-docs (> 1MB): Indexing overhead might not be worth it
- Can adjust threshold based on measurements

**Example**:
```
resources.resource1 (1KB) → Index ✅
resources.resource2 (1KB) → Index ✅
resources.largeResource (10MB) → Don't index, read full if needed ❌
```

### Fallback Strategy

**For large sub-documents**:
- Don't index them
- If needed, read entire snapshot (or use different strategy)
- Or: Break large sub-docs into smaller chunks

**For unindexed paths**:
- Check if path is within an indexed sub-doc
- If not, fall back to reading larger chunk or full snapshot

## Benefits

1. **Reduced I/O**: Read only relevant sub-documents
2. **Smaller index**: Fewer sub-docs than individual paths
3. **Natural grouping**: Related paths in same sub-doc
4. **Size control**: Can limit sub-doc size
5. **Scalable**: Works even with very large snapshots

## Challenges

1. **Sub-document boundaries**: How to determine natural boundaries?
2. **Size estimation**: How to estimate sub-doc size before writing? (without loading into memory)
3. **Index maintenance**: Must update index as snapshot changes
4. **Partial reads**: Must handle reading partial Tony documents (sub-docs)
5. **Streaming processing**: Must process snapshot incrementally, not load entire document
6. **State management**: Only keep current paths in memory, not entire state
7. **Tokenization from stream**: Must tokenize and parse from stream, not from memory buffer

## Recommendation

**Implement inverted index with sub-document indexing**:

1. **Break snapshot into sub-documents** at natural path boundaries
2. **Index sub-documents** up to size threshold (e.g., 1MB)
3. **Use inverted index** to find which sub-docs contain a path
4. **Read only relevant sub-documents** instead of entire snapshot

**This approach**:
- ✅ Aligns with inverted index pattern (like Elasticsearch)
- ✅ Handles giant virtual document (breaks into chunks)
- ✅ Reduces I/O (read only relevant sub-docs)
- ✅ Natural fit for controller use case (resources as sub-docs)
- ✅ Addresses the "whole DB is a document" problem

**Key Differences from Previous Approaches**:
- **Not path indexing**: We index sub-documents, not individual paths
- **Inverted index**: Like Elasticsearch, maps paths → sub-doc IDs
- **Sub-document boundaries**: Natural boundaries (e.g., at `resources.resource1`)
- **Size threshold**: Only index sub-docs up to certain size

**Next Steps**:
1. Design sub-document boundary detection (when to create sub-doc?)
2. Design inverted index structure (how to store path → sub-doc mapping?)
3. Design size threshold strategy (what size limit?)
4. **Design streaming snapshot write** (process diffs incrementally, don't load entire snapshot)
5. **Design streaming sub-document read** (read from offsets, tokenize from stream)
6. Implement sub-document indexing during snapshot write (streaming)
7. Implement read using inverted index (streaming)
8. Measure and tune size threshold

**Critical Implementation Requirements**:
- ✅ **No entire document in memory** - process incrementally
- ✅ **Streaming writes** - write sub-docs as we process diffs
- ✅ **Streaming reads** - read sub-docs from offsets using section readers
- ✅ **Tokenization from stream** - parse wire format from stream, not memory buffer
- ✅ **State management** - only keep current paths, not entire state
