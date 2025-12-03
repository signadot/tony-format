# Snapshot Path Indexing: Pros and Cons

## The Question

Can we index paths within snapshots at byte offsets, allowing us to read only the specific path we need from a snapshot, rather than reading the entire snapshot?

## Current Approach: Read Entire Snapshot

**Current Design**:
- Snapshot stored as full Tony document in `LogEntry.Diff`
- To read path `a.b.c`:
  1. Read entire snapshot (all paths)
  2. Extract path `a.b.c` from snapshot
  3. Use as base state

**Example**:
```
Snapshot at commit 16 (1MB document):
  Read entire snapshot → Extract a.b.c → Use as base state
```

## Proposed Approach: Index Paths Within Snapshots

**Proposed Design**:
- Snapshot stored as full Tony document
- Index paths within snapshot at byte offsets
- To read path `a.b.c`:
  1. Look up byte offset for `a.b.c` in snapshot
  2. Seek to offset and read only that path
  3. Use as base state

**Example**:
```
Snapshot at commit 16 (1MB document):
  Index: a.b.c → offset 12345
  Seek to offset 12345 → Read only a.b.c → Use as base state
```

## Analysis

### Option A: Index Paths Within Snapshots

**Structure**:
```go
type LogSegment struct {
    StartCommit int64
    EndCommit   int64
    // ... other fields
    IsSnapshot bool
    LogFile     string  // "A" or "B"
    LogPosition int64   // Byte offset of snapshot entry
    
    // NEW: Path-specific offsets within snapshot
    PathOffsets map[string]int64  // path → byte offset within snapshot
}

// OR: Separate index structure
type SnapshotPathIndex struct {
    SnapshotCommit int64
    SnapshotOffset int64  // Offset of snapshot entry in log file
    PathOffsets    map[string]int64  // path → byte offset within snapshot
}
```

**Read Process**:
1. Find snapshot ≤ commit
2. Look up path offset in snapshot index
3. Seek to offset and read only that path
4. Use as base state
5. Apply remaining diffs

### Pros

1. **Reduced I/O**
   - Read only needed path, not entire snapshot
   - Example: 1MB snapshot, 10KB path → read 10KB instead of 1MB
   - Significant I/O savings for large snapshots

2. **Faster Reads**
   - Less data to read
   - Less data to deserialize
   - Faster for reading specific paths

3. **Memory Efficiency**
   - Don't load entire snapshot into memory
   - Only load needed path
   - Better for large documents

4. **Scalability**
   - Works well even with very large snapshots
   - Can read small paths from huge snapshots
   - Better for documents with many paths

5. **Parallel Reads**
   - Can read multiple paths from same snapshot in parallel
   - Each path has its own offset
   - Better concurrency

### Cons

1. **Tony Format Limitation**
   - **Critical Question**: Can Tony format support seeking to specific paths?
   - Tony is a sequential format - can we seek to arbitrary offsets?
   - Need to understand Tony format structure

2. **Index Complexity**
   - Must build index of paths within snapshots
   - Must maintain offsets as snapshot is written
   - More complex index structure

3. **Index Size**
   - Index must store offset for each path in snapshot
   - Large documents → many paths → large index
   - Example: 1000 paths → 1000 offsets in index

4. **Write Complexity**
   - Must track offsets as writing snapshot
   - Must update index with path offsets
   - More complex snapshot creation

5. **Format Dependency**
   - Depends on Tony format supporting seeking
   - If format changes, offsets might break
   - Less flexible

6. **Partial Read Complexity**
   - Must handle reading partial Tony document
   - Must parse from offset (not from start)
   - More complex read logic

## Key Question: Can Tony Format Support Seeking?

**Answer: Yes, with some work**

**Requirements**:
1. **Use wire format**: Tony wire format supports seeking
2. **Tokenize and count brackets**: Scan tokens, count brackets to find document boundaries
3. **Offset counting with encoding**: Make offset counting work correctly with encoding

**Implementation Approach**:
- Tokenize Tony wire format
- Count brackets to identify document boundaries
- Track offsets as we tokenize
- Can seek to specific paths within document

**Feasibility**: ✅ **Feasible with implementation work**

**If Tony format supports seeking**:
- ✅ Path indexing is feasible
- ✅ Can read specific paths efficiently
- ✅ Significant I/O savings (see use case below)

## Alternative: Hybrid Approach

**Structure**:
- Snapshot stored as full Tony document
- Index stores offsets for "hot" paths (frequently accessed)
- For hot paths: read from offset
- For cold paths: read entire snapshot (or extract from already-read snapshot)

**Pros**:
- Best of both worlds
- Optimize for common cases
- Fall back to full read for uncommon paths

**Cons**:
- More complex
- Must track "hot" paths
- More logic to decide which approach to use

## Comparison Table

| Aspect | Read Entire Snapshot | Index Paths in Snapshot | Hybrid |
|--------|---------------------|------------------------|--------|
| **I/O Cost** | ❌ High (read all) | ✅ Low (read only path) | ⚠️ Medium (depends) |
| **Memory Cost** | ❌ High (load all) | ✅ Low (load only path) | ⚠️ Medium (depends) |
| **Read Speed** | ❌ Slower | ✅ Faster | ⚠️ Depends |
| **Index Complexity** | ✅ Simple | ❌ Complex | ⚠️ Medium |
| **Write Complexity** | ✅ Simple | ❌ Complex | ⚠️ Medium |
| **Format Dependency** | ✅ None | ❌ Requires seeking | ⚠️ Partial |
| **Scalability** | ❌ Poor (large docs) | ✅ Good | ✅ Good |

## Analysis: When Is Path Indexing Worth It?

### Use Case: Controller with Many Resources

**Scenario**: Controller managing thousands to tens of thousands of documents as children of one node.

**Scale**:
- Snapshot size: Very large (contains all resources)
- Single resource size: Small (individual document)
- **Ratio: ~10^6:1** snapshot size to single resource size

**Example**:
- Snapshot: 1GB (contains 10,000 resources)
- Single resource: ~1KB
- **Path indexing**: Read 1KB instead of 1GB → **Massive benefit (1000x reduction)**

**This use case makes path indexing extremely valuable!**

### Factors

1. **Snapshot Size**
   - Small (< 100KB): Path indexing overhead not worth it
   - Medium (100KB - 10MB): Path indexing might help
   - Large (> 10MB): Path indexing very beneficial
   - **Very Large (> 100MB)**: Path indexing **essential** (use case above)

2. **Path Size vs Snapshot Size**
   - Small paths in large snapshot: Path indexing **very beneficial**
   - **10^6:1 ratio**: Path indexing **essential** (use case above)
   - Large paths in small snapshot: Path indexing less beneficial
   - Similar sizes: Path indexing less beneficial

3. **Read Pattern**
   - Mostly full reads: Path indexing not helpful
   - **Mostly specific resources**: Path indexing **very helpful** (use case above)
   - Mix: Hybrid approach might work

4. **Tony Format Support**
   - ✅ **Supports seeking** (with implementation work)
   - Path indexing **feasible and valuable**

### Example Scenarios

**Scenario 1: Controller Use Case** ⭐
- Snapshot: 1GB document (10,000 resources)
- Path: 1KB resource
- **Path indexing**: Read 1KB instead of 1GB → **1000x I/O reduction, essential**

**Scenario 2: Large Document, Small Paths**
- Snapshot: 100MB document
- Path: 10KB subtree
- **Path indexing**: Read 10KB instead of 100MB → **Very beneficial**

**Scenario 3: Small Document**
- Snapshot: 100KB document
- Path: 50KB subtree
- **Path indexing**: Read 50KB instead of 100KB → **Less beneficial**

**Scenario 4: Full Reads**
- Snapshot: 10MB document
- Read: Entire document
- **Path indexing**: No benefit (must read all anyway)

## Implementation Considerations

### Implementation Details (Tony Format Seeking)

**Tony Format Seeking Requirements**:
1. **Wire format**: Use Tony wire format (supports seeking)
2. **Tokenization**: Tokenize input to identify structure
3. **Bracket counting**: Count brackets to find document boundaries
4. **Offset tracking**: Track offsets as we tokenize, accounting for encoding

**Index Structure**:
```go
type SnapshotPathIndex struct {
    SnapshotCommit int64
    SnapshotOffset int64  // Offset of snapshot entry in log file
    PathOffsets    map[string]int64  // kindedPath → byte offset within snapshot
}

// Example:
// Snapshot at commit 16, offset 1000
// PathOffsets: {
//   "resources.resource1": 1500,  // Offset within snapshot (relative to snapshot start)
//   "resources.resource2": 2000,
//   ...
//   // 10,000 resources indexed
// }
```

**Offset Tracking During Write**:
```go
// Write snapshot, tracking offsets
func writeSnapshotWithOffsets(writer io.Writer, snapshot *ir.Node) map[string]int64 {
    pathOffsets := make(map[string]int64)
    snapshotStart := writer.Position()
    
    // Write snapshot using wire format
    // As we write each path, track its offset
    writeNodeWithOffsets(writer, snapshot, "", snapshotStart, pathOffsets)
    
    return pathOffsets
}

// Recursively write node, tracking offsets for each path
func writeNodeWithOffsets(writer io.Writer, node *ir.Node, prefix string, 
                          snapshotStart int64, pathOffsets map[string]int64) {
    currentOffset := writer.Position() - snapshotStart
    
    // Record offset for this path
    if prefix != "" {
        pathOffsets[prefix] = currentOffset
    }
    
    // Write node using wire format
    // Tokenize and count brackets as we write
    // Track offsets accounting for encoding
    // ...
}
```

**Read Logic**:
```go
// Find snapshot
snapshot := findSnapshot(commit)

// Look up path offset
pathOffset := snapshot.PathOffsets[kindedPath]

// Seek to snapshot start + path offset
reader.Seek(snapshot.LogPosition + snapshotEntryLength + pathOffset)

// Read path using wire format tokenization
// Tokenize from offset, count brackets to find path boundary
pathData := readPathFromOffset(reader, pathOffset)

// Use as base state
state := pathData
```

**Read Implementation**:
```go
// Read path from offset using wire format
func readPathFromOffset(reader io.ReaderAt, offset int64) (*ir.Node, error) {
    // Seek to offset
    reader.Seek(offset)
    
    // Tokenize wire format from this offset
    tokens := tokenizeWireFormat(reader)
    
    // Count brackets to find document boundary for this path
    // Parse path from tokens
    pathNode := parsePathFromTokens(tokens)
    
    return pathNode, nil
}
```

**Write Logic**:
```go
// Write snapshot with path offset tracking
func writeSnapshot(logFile *os.File, snapshot *ir.Node, commit int64) error {
    snapshotOffset := logFile.Position()
    
    // Write snapshot using wire format
    // Track offsets as we write, accounting for encoding
    pathOffsets := writeSnapshotWithOffsets(logFile, snapshot)
    
    // Update index with path offsets
    index.AddSnapshotPathIndex(commit, snapshotOffset, pathOffsets)
    
    return nil
}
```

**Key Implementation Points**:
1. **Wire format**: Use Tony wire format for seeking support
2. **Tokenization**: Tokenize as we write to identify structure
3. **Bracket counting**: Count brackets to find document boundaries
4. **Offset tracking**: Track offsets accounting for encoding overhead
5. **Index storage**: Store path → offset mapping in index

### Implementation Challenges

1. **Tony Format Parsing from Offset**
   - ✅ **Solution**: Use wire format + tokenization + bracket counting
   - Must parse from arbitrary offset
   - Must understand format structure
   - Must handle nested structures
   - **Work required**: Implement tokenization and bracket counting

2. **Offset Tracking with Encoding**
   - ✅ **Solution**: Track offsets as we tokenize, accounting for encoding
   - Must track offsets as writing
   - Must account for format overhead
   - Must handle variable-length encoding
   - **Work required**: Make offset counting work with encoding

3. **Index Maintenance**
   - Must update index with path offsets
   - Must handle index size (many paths - 10,000+ for controller use case)
   - Must handle index updates
   - **Consideration**: Index size might be large, but acceptable given I/O savings

4. **Read Complexity**
   - Must seek to offset
   - Must parse partial document using tokenization
   - Must handle edge cases
   - **Work required**: Implement read-from-offset logic

**Summary**: Implementation work is required, but benefits (1000x I/O reduction) justify the effort.

## Recommendation

**✅ Strongly Recommend Path Indexing**

**Rationale**:
1. **Tony format supports seeking** (with implementation work)
2. **Use case is compelling**: 10^6:1 ratio makes path indexing essential
3. **Massive I/O savings**: 1000x reduction in I/O for controller use case
4. **Implementation work is justified**: Benefits far outweigh costs

**Implementation Plan**:

1. **Tony Format Seeking Support**:
   - Use wire format
   - Implement tokenization and bracket counting
   - Implement offset counting with encoding
   - Can seek to specific paths within document

2. **Path Indexing**:
   - Build index of paths → offsets as writing snapshot
   - Store offsets in index (e.g., `LogSegment.PathOffset` or separate structure)
   - Read logic: Look up offset, seek, read only that path

3. **Hybrid Approach** (optional):
   - Index all paths in snapshot
   - For full reads: Can still read entire snapshot
   - For path reads: Use offset to read only needed path

**Key Questions Answered**:
1. ✅ **Does Tony format support seeking?** Yes, with wire format + tokenization + bracket counting
2. ✅ **Can we parse a path from an offset?** Yes, with implementation work
3. ⚠️ **What's the overhead?** Need to measure, but likely small compared to I/O savings
4. ✅ **What's the typical ratio?** ~10^6:1 for controller use case (very compelling)

**Next Steps**:
1. ✅ **Tony format seeking**: Implement wire format tokenization and bracket counting
2. ✅ **Offset tracking**: Implement offset counting with encoding during snapshot write
3. ✅ **Path indexing**: Build index of paths → offsets
4. ✅ **Read optimization**: Use offsets to read only needed paths
5. ⚠️ **Measure**: Compare I/O savings vs complexity cost (but benefits are clear)

## Comparison to Existing Databases

**See `json_indexing_survey.md` for detailed survey of how existing databases handle JSON indexing.**

**See `inverted_index_subdocuments.md` for our proposed approach using inverted indexes with sub-document indexing.**

## Updated Approach: Inverted Index + Sub-Documents

**Key Insight**: Our use case is different - we have ONE giant virtual document (the whole DB), not many separate documents.

**New Approach**:
1. **Break snapshot into sub-documents** (chunks at natural path boundaries)
2. **Index sub-documents** using inverted index (path → sub-doc IDs)
3. **Read only relevant sub-documents** (not entire snapshot)
4. **Size threshold**: Index sub-docs up to certain size (e.g., 1MB)

**Benefits**:
- ✅ Aligns with inverted index pattern (like Elasticsearch)
- ✅ Handles giant virtual document (breaks into chunks)
- ✅ Reduces I/O (read only relevant sub-docs)
- ✅ Natural fit for controller use case (resources as sub-docs)

**See `inverted_index_subdocuments.md` for detailed design.**

### Key Findings from Survey

1. **Document Stores** (MongoDB, PostgreSQL JSONB):
   - Store complete documents
   - Read entire documents even for specific paths
   - **Not optimal** for our use case (large documents, small paths)

2. **Key-Value Stores** (FoundationDB, RocksDB):
   - Store paths as separate keys
   - Support partial reads naturally
   - **Very similar** to our path indexing approach
   - **Optimal** for our use case

3. **Our Approach**:
   - Similar to key-value stores
   - Index paths at byte offsets within snapshot
   - Read only specific paths
   - **Natural fit** for controller use case

**Conclusion**: Path indexing aligns with proven patterns from key-value stores, which excel at partial reads for large documents with small paths.

## Conclusion

Path indexing within snapshots **is highly recommended** for the controller use case.

**Key Findings**:
1. ✅ **Tony format supports seeking** (with wire format + tokenization + bracket counting)
2. ✅ **Use case is compelling**: 10^6:1 ratio (snapshot:resource) makes path indexing essential
3. ✅ **Massive I/O savings**: 1000x reduction in I/O for controller use case
4. ✅ **Implementation work is justified**: Benefits far outweigh implementation costs
5. ✅ **Aligned with proven patterns**: Similar to key-value stores (FoundationDB, RocksDB)

**Recommendation**: **Implement path indexing** for snapshots. The controller use case (thousands of resources, 10^6:1 ratio) makes this optimization essential, not optional.

**Implementation Priority**: **High** - This optimization is critical for the controller use case.
