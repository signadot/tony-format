# Snapshot Implementation Plan: 2-Level Index + Full-State

## Overview

**Backend**: Two `ir.Node` structures:
1. **Index Node**: Mirrors snapshot structure, contains `!snap-offset` (and optionally `!snap-size`) tags
2. **Data Node**: Full snapshot state

**Implementation**: In-memory first (both nodes loaded), streaming later.

## Work Estimation

### Phase 1: Core Infrastructure (Low-Medium Complexity)

#### 1.1 Index Node Building (with Range Descriptors)
**Work**: ~4-5 days
- Function to build index node from snapshot node
- Traverse snapshot in pre-order DFS
- Track byte offsets as snapshot is written
- **Calculate encoded size for each path**
- **Apply size threshold** (e.g., 4KB) - only index paths above threshold
- **Create range descriptors** for containers with unindexed children
- Create index node mirroring structure with offset tags and range tags

**Functions**:
```go
// Build index node from snapshot node structure
// Only indexes paths above sizeThreshold
func BuildSnapshotIndex(snapshotNode *ir.Node, sizeThreshold int64) (*ir.Node, error)

// Extract offset from index node (returns error if path not indexed)
func ExtractOffsetFromIndex(indexNode *ir.Node, kindedPath string) (int64, error)

// Get range descriptor for a path (if it's in an unindexed range)
func GetRangeFromIndex(indexNode *ir.Node, kindedPath string) (startOffset int64, endOffset int64, threshold int64, err error)

// Check if path is directly indexed
func IsPathIndexed(indexNode *ir.Node, kindedPath string) bool
```

#### 1.2 Index Node Navigation (with Range Support)
**Work**: ~2-3 days
- Navigate index node using `GetKPath()`
- Extract offset from tagged values (for indexed paths)
- Extract range descriptors (for unindexed ranges)
- Handle missing paths (may be in range)
- **Scan logic**: Find range start, scan tokens to locate path

**Functions**:
```go
// Get offset for a path from index node (if directly indexed)
func (idx *SnapshotIndex) GetPathOffset(kindedPath string) (int64, error)

// Get range descriptor for a path's parent container
func (idx *SnapshotIndex) GetRange(kindedPath string) (startOffset int64, endOffset int64, threshold int64, err error)

// Check if path is directly indexed
func (idx *SnapshotIndex) IsIndexed(kindedPath string) bool

// Scan from range start to find a path (for unindexed paths)
func (idx *SnapshotIndex) ScanToPath(rangeStart int64, rangeEnd int64, targetPath string, dataReader io.ReaderAt) (*ir.Node, error)
```

#### 1.3 Snapshot Storage Format
**Work**: ~1-2 days
- Define format: [Index Node][Data Node]
- Write both nodes to dlog
- Read both nodes from dlog
- Track positions

**Functions**:
```go
// Write snapshot (index + data) to dlog
func WriteSnapshotWithIndex(indexNode, dataNode *ir.Node, dlog *DLog) (indexPos, dataPos int64, err error)

// Read snapshot (index + data) from dlog
func ReadSnapshotWithIndex(indexPos, dataPos int64, dlog *DLog) (*ir.Node, *ir.Node, error)
```

**Total Phase 1**: ~6-8 days (increased due to range descriptor complexity)

---

### Phase 2: SnapshotReader Implementation (Medium Complexity)

#### 2.1 In-Memory SnapshotReader (with Range Scanning)
**Work**: ~3-4 days
- Load both index and data nodes into memory
- Implement `ReadPath()`:
  - **If path is indexed**: Use offset, extract from data node
  - **If path not indexed**: Find parent range, scan from range start
- Implement `StreamPathTo()` - encode path from data node (or scan and encode)
- Handle path not found cases
- **Scanning logic**: Parse tokens from range start, navigate to target path

**Functions**:
```go
type inMemorySnapshotReader struct {
    indexNode *ir.Node
    dataNode  *ir.Node
    commit    int64
    threshold int64  // Size threshold used for indexing
}

func (r *inMemorySnapshotReader) ReadPath(kindedPath string) (*ir.Node, error)
func (r *inMemorySnapshotReader) StreamPathTo(kindedPath string, w io.Writer) error
func (r *inMemorySnapshotReader) Close() error

// Internal: Scan from range start to find path
func (r *inMemorySnapshotReader) scanToPath(rangeStart int64, targetPath string) (*ir.Node, error)
```

**Total Phase 2**: ~3-4 days (increased due to range scanning)

---

### Phase 3: SnapshotWriter Implementation (Medium Complexity)

#### 3.1 In-Memory SnapshotWriter (with Size Tracking)
**Work**: ~3-4 days
- Accept snapshot data node
- **Encode data node to buffer** (to calculate sizes)
- **Calculate encoded size for each path** during encoding
- **Apply size threshold** - only index paths above threshold
- **Build index node** with offsets and range descriptors
- Write both nodes to dlog
- Return positions

**Functions**:
```go
type inMemorySnapshotWriter struct {
    dataNode     *ir.Node
    dlog         *DLog
    logFile      LogFileID
    threshold    int64  // Size threshold for indexing
}

func (w *inMemorySnapshotWriter) WriteSnapshot(dataNode *ir.Node) error
func (w *inMemorySnapshotWriter) Close() (indexPos, dataPos int64, err error)

// Internal: Encode node and track offsets/sizes
func encodeWithTracking(node *ir.Node, threshold int64) ([]byte, map[string]PathInfo, map[string]RangeInfo, error)

type PathInfo struct {
    Offset int64
    Size   int64
}

type RangeInfo struct {
    Start     int64
    End       int64  // -1 if unknown
    Threshold int64
}
```

**Challenge**: Need to encode data node to calculate sizes. Options:
- **Encode to buffer first** (recommended): Calculate sizes, then write
- Stream encode with size estimation (less accurate, more complex)

**Total Phase 3**: ~3-4 days (increased due to size calculation and range descriptors)

---

### Phase 4: PatchApplier Implementation (Medium Complexity)

#### 4.1 In-Memory PatchApplier
**Work**: ~3-4 days
- Read base snapshot (index + data nodes)
- Apply patches to data node using `ir.Merge()` or similar
- Rebuild index node for patched data node
- Return new SnapshotReader

**Functions**:
```go
type inMemoryPatchApplier struct {
    baseIndexNode *ir.Node
    baseDataNode  *ir.Node
    patches       []*ir.Node
}

func (a *inMemoryPatchApplier) Apply(baseSnapshot SnapshotReader, patches []*ir.Node) (SnapshotReader, error)
```

**Challenge**: Need patch application logic (doesn't exist yet - `tx.MergePatches()` only merges patches, doesn't apply to base)

**Total Phase 4**: ~3-4 days

---

### Phase 5: Integration with DLog Interfaces (Low Complexity)

#### 5.1 DLogSnapshot Implementation
**Work**: ~1-2 days
- Implement `ReadSnapshot()` - read index + data, return SnapshotReader
- Implement `WriteSnapshot()` - return SnapshotWriter
- Handle position tracking

**Functions**:
```go
func (dl *DLog) ReadSnapshot(logFile LogFileID, indexPos int64) (SnapshotReader, error)
func (dl *DLog) WriteSnapshot() (SnapshotWriter, error)
```

**Total Phase 5**: ~1-2 days

---

### Phase 6: Snapshotter Implementation (Low-Medium Complexity)

#### 6.1 Complete Snapshotting Workflow
**Work**: ~2-3 days
- Implement `CreateSnapshot()` workflow:
  1. Read base snapshot (or start empty)
  2. Apply patches
  3. Write new snapshot with index
  4. Return positions

**Functions**:
```go
func (s *Snapshotter) CreateSnapshot(baseLogFile LogFileID, baseIndexPos int64, patches []*ir.Node) (logFile LogFileID, indexPos int64, err error)
```

**Total Phase 6**: ~2-3 days

---

## Total Work Estimate

**In-Memory Implementation**: ~18-26 days (~4-5 weeks)

**Breakdown**:
- Core Infrastructure: 6-8 days (increased due to range descriptors)
- SnapshotReader: 3-4 days (increased due to range scanning)
- SnapshotWriter: 3-4 days (increased due to size calculation)
- PatchApplier: 3-4 days (includes patch application logic)
- DLog Integration: 1-2 days
- Snapshotter: 2-3 days

**Note**: Range descriptors add ~4-5 days of complexity but are essential for handling large arrays with millions of elements.

**Dependencies**:
- Need patch application logic (applying patch to base state)
- Need to track offsets during encoding (or encode to buffer)

---

## Proposed Interface

### Core Types

```go
// SnapshotIndex wraps the index node and provides lookup
type SnapshotIndex struct {
    indexNode *ir.Node  // Index node with !snap-offset tags
    dataSize  int64     // Total size of data node (for last path size)
}

// SnapshotData wraps the data node
type SnapshotData struct {
    dataNode *ir.Node  // Full snapshot state
}
```

### Index Building

```go
// BuildIndexNode creates an index node from a snapshot data node
// Tracks offsets as the data node would be encoded
func BuildIndexNode(dataNode *ir.Node) (*ir.Node, map[string]PathInfo, error)

type PathInfo struct {
    Offset int64  // Byte offset in encoded data node
    Size   int64  // Size of this path's data
}

// BuildIndexNodeFromOffsets creates index node structure with given offsets and ranges
func BuildIndexNodeFromOffsets(dataNode *ir.Node, offsets map[string]PathInfo, ranges map[string]RangeInfo) (*ir.Node, error)
```

### Index Navigation (with Range Support)

```go
// GetPathOffset extracts offset from index node for a given path (if directly indexed)
func (idx *SnapshotIndex) GetPathOffset(kindedPath string) (offset int64, err error)

// GetRange gets range descriptor for a path's parent container
func (idx *SnapshotIndex) GetRange(kindedPath string) (startOffset int64, endOffset int64, threshold int64, err error)

// IsIndexed checks if path is directly indexed
func (idx *SnapshotIndex) IsIndexed(kindedPath string) bool

// ScanToPath scans from range start to find a path (for unindexed paths)
func (idx *SnapshotIndex) ScanToPath(rangeStart int64, rangeEnd int64, targetPath string, dataReader io.ReaderAt) (*ir.Node, error)

// ListPaths returns all indexed paths (pre-order DFS)
func (idx *SnapshotIndex) ListPaths() ([]string, error)
```

### Snapshot Storage

```go
// WriteSnapshotPair writes both index and data nodes to dlog
// Returns positions where each was written
func WriteSnapshotPair(
    dlog *DLog,
    logFile LogFileID,
    indexNode *ir.Node,
    dataNode *ir.Node,
) (indexPos int64, dataPos int64, err error)

// ReadSnapshotPair reads both index and data nodes from dlog
func ReadSnapshotPair(
    dlog *DLog,
    logFile LogFileID,
    indexPos int64,
    dataPos int64,
) (indexNode *ir.Node, dataNode *ir.Node, err error)
```

### SnapshotReader Implementation

```go
type inMemorySnapshotReader struct {
    index   *SnapshotIndex
    data    *SnapshotData
    commit  int64
}

func NewInMemorySnapshotReader(indexNode, dataNode *ir.Node, commit int64) SnapshotReader {
    return &inMemorySnapshotReader{
        index: &SnapshotIndex{indexNode: indexNode},
        data:  &SnapshotData{dataNode: dataNode},
        commit: commit,
    }
}

func (r *inMemorySnapshotReader) ReadPath(kindedPath string) (*ir.Node, error) {
    // Check if path is directly indexed
    if r.index.IsIndexed(kindedPath) {
        // Use offset from index (or just GetKPath for in-memory)
        return r.data.dataNode.GetKPath(kindedPath)
    }
    
    // Path not indexed - find parent range
    parentPath := getParentPath(kindedPath)
    startOffset, endOffset, threshold, err := r.index.GetRange(parentPath)
    if err != nil {
        return nil, err  // No range, path doesn't exist
    }
    
    // Scan from range start to find path
    // For in-memory, we can still use GetKPath (scanning is for streaming)
    // But we should verify the path exists within the range
    return r.data.dataNode.GetKPath(kindedPath)
}

func (r *inMemorySnapshotReader) StreamPathTo(kindedPath string, w io.Writer) error {
    node, err := r.ReadPath(kindedPath)
    if err != nil {
        return err
    }
    if node == nil {
        return nil // Path doesn't exist
    }
    return encode.Encode(node, w)
}

func (r *inMemorySnapshotReader) Close() error {
    r.index = nil
    r.data = nil
    return nil
}
```

### SnapshotWriter Implementation

```go
type inMemorySnapshotWriter struct {
    dlog     *DLog
    logFile  LogFileID
    dataNode *ir.Node
}

func NewInMemorySnapshotWriter(dlog *DLog, logFile LogFileID) SnapshotWriter {
    return &inMemorySnapshotWriter{
        dlog: dlog,
        logFile: logFile,
    }
}

func (w *inMemorySnapshotWriter) WriteSnapshot(reader SnapshotReader) error {
    // For in-memory, read full snapshot
    dataNode, err := reader.ReadPath("") // Read root
    if err != nil {
        return err
    }
    w.dataNode = dataNode
    return nil
}

func (w *inMemorySnapshotWriter) Close() (logPosition int64, err error) {
    // Encode data node to calculate sizes
    encodedData, offsets, ranges, err := encodeWithTracking(w.dataNode, w.threshold)
    if err != nil {
        return 0, err
    }
    
    // Build index node with offsets and ranges
    indexNode, err := BuildIndexNodeFromOffsets(w.dataNode, offsets, ranges)
    if err != nil {
        return 0, err
    }
    
    // Write both nodes
    indexPos, dataPos, err := WriteSnapshotPair(w.dlog, w.logFile, indexNode, encodedData)
    if err != nil {
        return 0, err
    }
    
    // Return index position (caller uses this as SnapPos)
    return indexPos, nil
}
```

### PatchApplier Implementation

```go
type inMemoryPatchApplier struct {
    baseIndexNode *ir.Node
    baseDataNode  *ir.Node
    patches       []*ir.Node
}

func NewInMemoryPatchApplier() PatchApplier {
    return &inMemoryPatchApplier{}
}

func (a *inMemoryPatchApplier) Apply(baseSnapshot SnapshotReader, patches []*ir.Node) (SnapshotReader, error) {
    // Read base snapshot (in-memory)
    baseReader, ok := baseSnapshot.(*inMemorySnapshotReader)
    if !ok {
        return nil, fmt.Errorf("base snapshot must be in-memory")
    }
    
    // Clone base data node
    patchedData := baseReader.data.dataNode.Clone()
    
    // Apply each patch (need patch application logic)
    for _, patch := range patches {
        patchedData, err = ApplyPatch(patchedData, patch)
        if err != nil {
            return nil, err
        }
    }
    
    // Build new index for patched data
    indexNode, _, err := BuildIndexNode(patchedData)
    if err != nil {
        return nil, err
    }
    
    // Return new reader
    return NewInMemorySnapshotReader(indexNode, patchedData, baseReader.commit), nil
}

// ApplyPatch applies a patch to a base node (needs implementation)
func ApplyPatch(base *ir.Node, patch *ir.Node) (*ir.Node, error) {
    // TODO: Implement patch application logic
    // This is the missing piece - how to merge patch into base
}
```

### DLogSnapshot Implementation

```go
func (dl *DLog) ReadSnapshot(logFile LogFileID, indexPos int64) (SnapshotReader, error) {
    // Read index node
    indexEntry, err := dl.ReadEntryAt(logFile, indexPos)
    if err != nil {
        return nil, err
    }
    
    // Extract data position from index node (or read separately)
    // For now, assume we store both positions somewhere
    // Or: index node contains data position as metadata
    
    // Read both nodes
    indexNode, dataNode, err := ReadSnapshotPair(dl, logFile, indexPos, dataPos)
    if err != nil {
        return nil, err
    }
    
    return NewInMemorySnapshotReader(indexNode, dataNode, indexEntry.Commit), nil
}

func (dl *DLog) WriteSnapshot() (SnapshotWriter, error) {
    inactiveLog := dl.GetInactiveLog()
    return NewInMemorySnapshotWriter(dl, inactiveLog), nil
}
```

### Snapshotter Implementation

```go
type Snapshotter struct {
    dlog *DLog
}

func (s *Snapshotter) CreateSnapshot(
    baseLogFile LogFileID,
    baseIndexPos int64,
    patches []*ir.Node,
) (logFile LogFileID, indexPos int64, err error) {
    // 1. Read base snapshot (or start empty)
    var baseSnapshot SnapshotReader
    if baseIndexPos >= 0 {
        baseSnapshot, err = s.dlog.ReadSnapshot(baseLogFile, baseIndexPos)
        if err != nil {
            return "", 0, err
        }
        defer baseSnapshot.Close()
    } else {
        // Empty snapshot
        emptyNode := ir.NewObjectNode()
        indexNode, _, _ := BuildIndexNode(emptyNode)
        baseSnapshot = NewInMemorySnapshotReader(indexNode, emptyNode, 0)
    }
    
    // 2. Apply patches
    applier := NewInMemoryPatchApplier()
    patchedSnapshot, err := applier.Apply(baseSnapshot, patches)
    if err != nil {
        return "", 0, err
    }
    defer patchedSnapshot.Close()
    
    // 3. Write new snapshot
    writer := s.dlog.WriteSnapshot()
    err = writer.WriteSnapshot(patchedSnapshot)
    if err != nil {
        return "", 0, err
    }
    
    indexPos, err = writer.Close()
    if err != nil {
        return "", 0, err
    }
    
    inactiveLog := s.dlog.GetInactiveLog()
    return inactiveLog, indexPos, nil
}
```

---

## Key Functions Summary

### Index Building
1. `BuildIndexNode(dataNode *ir.Node) (*ir.Node, map[string]PathInfo, error)`
2. `BuildIndexNodeFromOffsets(dataNode *ir.Node, offsets map[string]PathInfo) (*ir.Node, error)`

### Index Navigation
3. `GetPathOffset(indexNode *ir.Node, kindedPath string) (offset int64, size int64, error)`
4. `ListPaths(indexNode *ir.Node) ([]string, error)`

### Storage
5. `WriteSnapshotPair(dlog, logFile, indexNode, dataNode) (indexPos, dataPos int64, error)`
6. `ReadSnapshotPair(dlog, logFile, indexPos, dataPos) (indexNode, dataNode *ir.Node, error)`

### Patch Application (Missing - needs implementation)
7. `ApplyPatch(base *ir.Node, patch *ir.Node) (*ir.Node, error)`

### Offset Tracking During Encoding (with Size Calculation)
8. `EncodeWithOffsetTracking(node *ir.Node, threshold int64) ([]byte, map[string]PathInfo, map[string]RangeInfo, error)`
9. `CalculatePathSize(node *ir.Node, path string) (int64, error)` - Calculate encoded size of a path

---

## Open Questions

1. **Patch Application**: How to apply patch to base? (needs design/implementation)
2. **Size Threshold**: What default threshold? (recommend 4KB)
3. **Range End Detection**: Store `!snap-range-end` explicitly, or use next indexed path?
4. **Position Storage**: How to store both index and data positions? (in Entry? separate?)
5. **Index Size**: What if index node > 1M even with range descriptors? (increase threshold dynamically? split index?)
6. **Scanning Performance**: For unindexed paths, how to optimize scanning? (token-based? incremental parser?)
7. **Nested Ranges**: How to handle ranges within ranges? (e.g., large array containing another large array)
