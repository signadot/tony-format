# Storage Redesign: Final Design

## Overview

This document describes the final design for the storage system. It is a reference document for implementation and future maintenance.

**📐 Implementation Approach**: Bottom-up implementation with circular dependencies resolved at design level. See `implementation_strategy.md` for:
- Complete design and implementation flow
- Layer structure (Foundation → Write → Read → Compaction & Snapshots)
- Design decisions needed before implementation
- Implementation order (bottom-up, no circular dependencies in code)

**Current Status**: Foundation (Layer 1) mostly complete. Design decisions needed for Layers 2-4 before proceeding with implementation.

## Core Design Principles

1. **Simplicity**: Prefer simple solutions over complex ones
2. **Consistency**: Consistent structure (all entries at root)
3. **Efficiency**: Minimize storage and read overhead
4. **Correctness**: Logs are source of truth, index is optimization
5. **Recoverability**: Can rebuild index from logs

## Storage Layout

### LogA/LogB Double Buffering

- **Two files (double-buffered)**: `logA` and `logB` (alternating active log)
- **Single level**: All data stored in single level (no level promotion needed)
- **Format**: Sequential append-only logs
- **LogPosition**: Byte offset within log file
- **Index**: Maps `(path, logFile)` → `(logPosition, commitRange, kindedPath)`
- **Active Log**: One log is active for writes, the other is inactive (being snapshotted)

### Rationale

- **Maintains monotonicity**: Snapshot always appended after all diffs in same log
- **No write blocking**: Can write to active log while snapshotting inactive log
- **Simple snapshot write**: Just append to inactive log (no coordination needed)
- **Natural separation**: Active vs inactive log is clear
- **Parallel compaction**: Can snapshot inactive log while writing to active log

### Active Log Switching

**Initial State**: Active = LogA

**When Compaction Boundary Reached** (e.g., commit 16, 32, 64, ...):
1. Switch active log (A → B or B → A)
2. Compute snapshot from inactive log (all diffs up to boundary)
3. Append snapshot to inactive log (after all diffs)
4. Continue writing new diffs to active log

**Example Flow**:
```
Commits 1-15: Write diffs to logA
Commit 16: Compaction boundary reached
  - Switch active writes to logB
  - Snapshot logA (compute full state from commits 1-16)
  - Append snapshot to logA (after all diffs)
  - Continue writing commits 17+ to logB

Commits 17-31: Write diffs to logB
Commit 32: Next compaction boundary
  - Switch active writes to logA
  - Snapshot logB (compute full state from commits 17-32)
  - Append snapshot to logB (after all diffs)
  - Continue writing commits 33+ to logA
```

**File Structure**:
```
logA:
  commit 1: diff
  commit 2: diff
  ...
  commit 16: diff
  commit 16: snapshot  ← appended after all diffs, maintains monotonicity

logB:
  commit 17: diff
  commit 18: diff
  ...
  commit 32: diff
  commit 32: snapshot  ← appended after all diffs

logA: (active again)
  commit 33: diff
  commit 34: diff
  ...
```

## Log Entry Format

### Schema

```go
type LogEntry struct {
    Commit    int64      // Commit number (set when appended to log)
    Seq       int64      // Transaction sequence (txSeq)
    Timestamp string     // Timestamp
    Diff      *ir.Node   // Root diff (always at root, empty kinded path "") - contains all paths affected by this commit
}
```

### Key Design Decisions

- ✅ **No Path field**: Always root (empty kinded path ""), redundant to store
- ✅ **No Inputs field**: Computed from correctness criteria
- ✅ **No Level field**: All data in single level (no level concept)
- ✅ **No Pending field**: Removed (can add later if needed)
- ✅ **Root diff**: All entries written at root (empty kinded path "") with merged diffs

### Example

Writing `a.b` and `a.b.c` at commit 2 (active log is LogA):
- **Entry**: `{Commit: 2, Seq: 1, Timestamp: "...", Diff: {a: {b: {y: 2, c: {z: 3}}}}}`
- **Written to**: `logA` (active log)
- **Index**:
  - `a` → `{LogPosition: 200, KindedPath: "a", LogFile: "A", IsSnapshot: false}`
  - `a.b` → `{LogPosition: 200, KindedPath: "a.b", LogFile: "A", IsSnapshot: false}`
  - `a.b.c` → `{LogPosition: 200, KindedPath: "a.b.c", LogFile: "A", IsSnapshot: false}`

## Log File Format

### Length-Prefixed Entries

**Format**: `[4 bytes: uint32 length (big-endian)][entry data in Tony wire format]`

**Why Length-Prefixed**:
- Simpler random access (read length, read exact bytes)
- Easier recovery (can skip entries without parsing)
- Clearer boundaries (explicit entry boundaries)
- Negligible overhead (~4 bytes per entry, ~4% for typical entries)

### Reading

**Standard Reading** (full entry):
```go
// 1. Read 4 bytes → get length
lengthBytes := make([]byte, 4)
file.ReadAt(lengthBytes, offset)
length := binary.BigEndian.Uint32(lengthBytes)

// 2. Read length bytes → get entry data
entryBytes := make([]byte, length)
file.ReadAt(entryBytes, offset+4)

// 3. Parse entry data from Tony wire format
entry := LogEntryFromTony(bytes.NewReader(entryBytes))
```

**Streaming Reading** (for large entries/sub-documents):
```go
// 1. Read 4 bytes → get length
lengthBytes := make([]byte, 4)
file.ReadAt(lengthBytes, offset)
length := binary.BigEndian.Uint32(lengthBytes)

// 2. Create section reader for entry data
sectionReader := io.NewSectionReader(file, offset+4, int64(length))

// 3. Parse directly from TokenSource (streaming, no token collection)
source := token.NewTokenSource(sectionReader)
node, err := parse.ParseFromTokenSource(source)
```

**Rationale**: For large entries (snapshots), streaming avoids loading entire entry into memory. Parse directly from TokenSource without collecting tokens.

**Note**: `parse.ParseFromTokenSource` needs to be added to the `parse` package. It should:
- Take `*token.TokenSource` and `...ParseOption`
- Parse incrementally as tokens arrive from TokenSource
- Balance and parse tokens in a streaming fashion
- Return `*ir.Node`
- **Critical**: Must not collect all tokens first - must parse incrementally

### Writing

**Standard Writing** (small entries):
```go
// 1. Serialize entry to Tony format
entryBytes := LogEntryToTony(entry)

// 2. Write length prefix
lengthBytes := make([]byte, 4)
binary.BigEndian.PutUint32(lengthBytes, uint32(len(entryBytes)))
file.Write(lengthBytes)

// 3. Write entry
file.Write(entryBytes)
```

**Streaming Writing with Offset Tracking** (for snapshots with indexing):
```go
// 1. Write length prefix (placeholder, update after)
lengthPos := file.Position()
lengthBytes := make([]byte, 4)
file.Write(lengthBytes)

// 2. Create TokenSink with offset tracking
var pathOffsets map[string]int64
sink := token.NewTokenSink(file, func(offset int, path string, tok token.Token) {
    // Record path offsets for indexing
    pathOffsets[path] = int64(offset)
})

// 3. Tokenize and write entry
tokens := tokenizeEntry(entry)
sink.Write(tokens)

// 4. Update length prefix
entrySize := sink.Offset()
binary.BigEndian.PutUint32(lengthBytes, uint32(entrySize))
file.WriteAt(lengthBytes, lengthPos)
```

**Rationale**: For snapshots, offset tracking enables path indexing and inverted index building.

## Kinded Paths

### Package

**Location**: `go-tony/system/logd/storage/kindedpath`

**Rationale**: 
- Path operations are fundamental and may replace `go-tony/ir/path` in the future
- Separate package allows for future promotion
- Keeps path operations separate from storage-specific code

### Syntax

Kinded paths encode node kinds in the syntax:
- `a.b` → Object accessed via `.b` (a is ObjectType)
- `a[0]` → Dense Array accessed via `[0]` (a is ArrayType)
- `a{0}` → Sparse Array accessed via `{0}` (a is SparseArrayType)

See `go-tony/system/logd/api/kinded_paths.md` for full syntax specification.

### Operations

**`kindedpath/path.go`**:
- `Parse(kindedPath string) (*PathSegment, error)` - parse kinded path string
- `PathSegment.String()` - convert to kinded path string
- `PathSegment.Parent()` - get parent path
- `PathSegment.IsChildOf()` - check if child of parent
- `PathSegment.Compare()` - compare paths

**`kindedpath/extract.go`**:
- `Get(diff *ir.Node, kindedPath string) (*ir.Node, error)` - extract nested path from diff
- `ExtractAll(diff *ir.Node) (map[string]*ir.Node, error)` - extract all paths from diff

## Index Structure

### LogSegment

```go
type LogSegment struct {
    StartCommit int64
    StartTx     int64
    EndCommit   int64
    EndTx       int64
    KindedPath  string  // "a.b.c" (kinded path - used for querying and extraction from root diff; "" for root)
    LogPosition int64   // Byte offset in log file
    LogFile     string  // "A" or "B" - which log file contains this segment
    IsSnapshot  bool    // true if this segment is a snapshot (full state), false if diff
}
```

### Indexing Strategy

**Subtree-Inclusive Indexing**:
- All entries written at root (empty kinded path "") with merged diffs
- Index maps all paths to same entry with different `KindedPath`
- Same entry supports reading at parent and child levels
- Index tracks which log file (A or B) contains each segment

**Example**:
- Write `a.b` and `a.b.c` at commit 5 → ONE entry at root with `{a: {b: {y: 2, c: {z: 3}}}}`
- Active log is LogA
- Index `a` → `{LogPosition: 200, KindedPath: "a", LogFile: "A", IsSnapshot: false}`
- Index `a.b` → `{LogPosition: 200, KindedPath: "a.b", LogFile: "A", IsSnapshot: false}`
- Index `a.b.c` → `{LogPosition: 200, KindedPath: "a.b.c", LogFile: "A", IsSnapshot: false}`

**After Compaction** (commit 16):
- Snapshot appended to LogA
- Index snapshot → `{LogPosition: 5000, KindedPath: "", LogFile: "A", IsSnapshot: true}`

## Write Operations

### Transaction Buffer

**In-Memory Buffer Only**:
- Accumulate entries in in-memory buffer during transaction
- Atomically append to log on commit
- **Design constraint**: Transactions must fit in memory
- **Crash handling**: Client sees failure and retries (no transaction resumption)

### Write Flow

1. **During transaction**: Participants add patches independently (no contention)
2. **At commit** (single-threaded, serialized by commit):
   - Merge all patches into root diff
   - Determine active log (LogA or LogB)
   - Write ONE entry at root (empty kinded path "") with merged diff to active log
   - Index all paths to root entry with `KindedPath` and `LogFile` (active log)

3. **At compaction boundary**:
   - Switch active log
   - Compute snapshot from inactive log
   - Append snapshot to inactive log
   - Continue writing to new active log

### Write Contention

✅ **No additional contention**:
- Within transaction: No contention (participants add independently)
- At commit: Single-threaded merge (commits already serialized)
- Root path: Most work, but still single-threaded at commit time

## Read Operations

### Read Flow

**Detailed Algorithm**: See `read_algorithm.md` for complete specification.

**High-Level Steps**:

1. **Get Index Iterator**: Position iterator at `kindedPath` in index hierarchy
2. **Find Snapshot**: Find highest snapshot ≤ `commit` (check both LogA and LogB)
3. **Read Snapshot**: If snapshot exists, read snapshot and use as base state
4. **Iterate Commits After Snapshot**: Use iterator to iterate commits starting from snapshot commit+1 to `commit`:
   - Use `it.CommitsAt(snapshotCommit+1, Up)` to seek and iterate upward
   - Read entry at `LogPosition` from appropriate log file (LogA or LogB)
   - Extract diff using `KindedPath` (`kindedpath.Get(entry.Diff, segment.KindedPath)`)
   - Apply diff to result state (`applyDiff(result, diff)`)
   - Stop when commit reached
5. **Extract Final Path**: If needed, extract final path from result
6. **Return Result**: Return final state

**Key Optimization**: Uses snapshot as base state, then applies only diffs after snapshot. Checks both LogA and LogB to find snapshot and diffs.

### Key Operations

**Iterator-Based Iteration**: Uses `IndexIterator` to iterate commits at a specific path:
- `index.IterAtPath(kindedPath)` positions iterator at the path
- `it.CommitsAt(commit, Down)` seeks to commit and iterates downward (only segments <= commit)
- Stops early when all necessary segments are read
- No need to materialize all segments in memory

**Snapshot Finding**: 
- Snapshots created at exponential boundaries: commit 16, 32, 64, 128, 256, ...
- Find highest snapshot ≤ commit (check both LogA and LogB)
- Example: `commit=100` → Find snapshot at commit 64 (if exists)

**Diff Application**: 
- Merge diff into existing state
- Handles objects (merge fields), arrays (merge elements), deletions, insertions
- Snapshot initializes state, subsequent diffs merge into it

### Example

```go
// Read a.b.c at commit 100
index.RLock()
defer index.RUnlock()

it := index.IterAtPath("a.b.c")
if !it.Valid() {
    return nil, nil // Path doesn't exist
}

// Find snapshot <= commit 100
// Check both LogA and LogB for snapshots
var snapshot *LogSegment
for seg := range it.Snapshots() {
    if seg.EndCommit <= 100 && seg.IsSnapshot {
        if snapshot == nil || seg.EndCommit > snapshot.EndCommit {
            snapshot = seg
        }
    }
}

// Read snapshot if exists (e.g., snapshot at commit 64)
var result *ir.Node
if snapshot != nil {
    logFile := getLogFile(snapshot.LogFile) // "A" or "B"
    entry := ReadEntryAt(logFile, snapshot.LogPosition)
    result = kindedpath.Get(entry.Diff, snapshot.KindedPath) // Extract from snapshot
}

// Iterate commits after snapshot (65-100)
for seg := range it.CommitsAt(snapshot.EndCommit+1, Up) {
    if seg.EndCommit > 100 {
        break // Stop at commit 100
    }
    
    logFile := getLogFile(seg.LogFile) // "A" or "B"
    entry := ReadEntryAt(logFile, seg.LogPosition)
    // Entry.Diff = {a: {b: {y: 2, c: {z: 3}}}}
    
    diff := kindedpath.Get(entry.Diff, seg.KindedPath)  // Extract "a.b.c"
    // diff = {z: 3}
    
    result = applyDiff(result, diff)
}

// Result: final state at commit 100
```

### Edge Cases

- **Empty State**: No segments → return `nil`
- **Path Not Found**: Segment exists but path not in diff → skip segment
- **Multiple Segments**: Same commit → apply in order (sorted by `KindedPath`)
- **Corrupted Entry**: Read fails → return error
- **No Snapshot**: If no snapshot exists, start from empty state and apply all diffs
- **Log File Switching**: Must check both LogA and LogB to find all relevant segments

## Index Persistence

### Hybrid Approach

**Persist index with max commit number**:
- **Normal startup**: Load persisted index, scan logs from `MaxCommit + 1` forward (incremental rebuild)
- **Corruption/recovery**: Full rebuild from logs if index missing/corrupted
- **Rationale**: Fast startup (most of the time), logs are source of truth

### IndexMetadata

```go
type IndexMetadata struct {
    MaxCommit int64 // Highest commit number in the index
}
```

### Recovery

**Rebuild Index from Logs**:
1. For each log file (LogA and LogB):
   - Open log file
   - Create iterator starting at offset 0
   - For each entry:
     - If entry.Commit > maxCommit:
       - Extract all paths from diff (`kindedpath.ExtractAll`)
       - Determine if entry is snapshot (check commit against boundaries)
       - Index each path with `KindedPath`, `LogFile` (A or B), and `IsSnapshot`
     - Move to next entry
   - Close log file
2. Persist index with new MaxCommit

## Compaction & Snapshots

### Compaction Boundaries

- **Exponential boundaries**: commit 16, 32, 64, 128, 256, 512, 1024, ...
- **Formula**: `commit % (divisor^boundary) == 0` where boundary increases exponentially
- **Example**: With divisor=2, boundaries at 16 (2^4), 32 (2^5), 64 (2^6), etc.

### Single Level Design

- **All data in level 0**: No level promotion needed
- **Double-buffered**: `logA` and `logB` alternate
- **Snapshots at boundaries**: Created at exponential boundaries, appended to inactive log

### Snapshot Format

**Format**: Same as `LogEntry` - full state stored in `Diff` field
- Snapshots are `LogEntry` instances with complete state at compaction boundary
- Stored in same log files as diffs (simplifies index)
- Indexed with `LogSegment.IsSnapshot = true` flag

**Rationale**: Reuses existing write/read code, fits naturally in index structure.

### Snapshot Storage

**Storage**: Same log files as diffs
- Snapshots written using same `AppendEntry` operation
- `LogSegment.IsSnapshot` flag distinguishes snapshots from diffs
- Natural ordering: snapshots and diffs interleaved in log

**Benefits**:
- Simplifies index (one structure for both)
- Reuses write/read code
- Natural commit ordering

### Inverted Index with Sub-Documents

**Problem**: Large snapshots (1GB+) cannot be loaded entirely into memory. Need to break into sub-documents.

**Solution**: Inverted index with sub-document indexing (see `inverted_index_subdocuments.md`)

**Approach**:
1. Break snapshots into sub-documents at natural path boundaries (e.g., `resources.resource1`)
2. Index sub-documents up to size threshold (e.g., 1MB)
3. Use inverted index: path → sub-document IDs and offsets
4. Read only relevant sub-documents instead of entire snapshot

**Key Constraint**: Must process incrementally/streaming - cannot load entire snapshot into memory.

**Token Streaming Support**: ✅ **Available** - `TokenSource` enables streaming tokenization from `io.Reader`, `TokenSink` enables offset tracking during encoding.

### Path Indexing Within Snapshots

**Problem**: Controller use case has 10^6:1 ratio (1GB snapshot, 1KB resources). Need to read specific paths without reading entire snapshot.

**Solution**: Index paths within snapshots at byte offsets (see `snapshot_path_indexing.md`)

**Approach**:
1. Track byte offsets as writing snapshots (`TokenSink.Offset()`)
2. Detect node starts during encoding (`TokenSink` callback)
3. Store path → offset mapping in index
4. Read paths from offsets using `io.SectionReader` + `TokenSource`

**Token Streaming Support**: ✅ **Available** - `TokenSink` tracks offsets and detects node starts, `TokenSource` enables reading from arbitrary offsets.

### Compaction Strategy

**Status**: ✅ **Designed** - LogA/LogB double buffering with snapshots

**Compaction Process**:
1. **When boundary reached** (e.g., commit 16, 32, 64, ...):
   - Switch active log (A → B or B → A)
   - Compute snapshot from inactive log (read all diffs up to boundary, compute full state)
   - Append snapshot to inactive log (after all diffs)
   - Continue writing new diffs to active log

2. **Snapshot Format**: Same as `LogEntry` - full state in `Diff` field
   - `IsSnapshot = true` in index
   - Stored in same log file as diffs (inactive log)

3. **Benefits**:
   - Maintains monotonicity (snapshot always after diffs in same log)
   - No write blocking (can write to active log while snapshotting inactive log)
   - Simple logic (just alternate between two logs)

**Token Streaming Integration**:
- **Writing snapshots**: Use `TokenSink` to track offsets and build inverted index
- **Reading snapshots**: Use `TokenSource` with `io.SectionReader` to read sub-documents
- **Path indexing**: Use `TokenSink` callbacks to record path offsets during encoding

See `compaction_snapshot_design.md` for detailed design questions and approach.

## Error Handling

### Corrupted Entries

- **Partial writes**: Detect via ReadAt error (length > available bytes)
- **Corrupted entry data**: Parser fails → return error, skip entry in recovery
- **Recovery**: Skip corrupted entries, continue with next entry

### File Errors

- **Missing log files**: Create empty file
- **Disk full**: Return error, transaction fails (client retries)
- **Lock errors**: Retry with backoff

## Performance Considerations

### Storage Overhead

- **Length prefix**: 4 bytes per entry (~4% for typical 100-byte entries)
- **No Path field**: Saves ~10-20 bytes per entry
- **Root diff**: Single entry instead of multiple (saves space)

### Read Performance

- **Sequential reads**: Sort by LogPosition, read sequentially
- **Random access**: Length-prefixed allows direct access
- **Path extraction**: O(depth) traversal, typically fast

### Write Performance

- **In-memory buffer**: Fast accumulation
- **Single-threaded merge**: No contention, simple logic
- **Atomic append**: Single write operation

## Future Considerations

### Potential Improvements

1. **Compaction**: Design compaction strategy for new layout
2. **Compression**: Compress log entries (if needed)
3. **Replication**: Add replication support (if needed)
4. **Query optimization**: Optimize index queries (if needed)

### Package Promotion

The `kindedpath` package may be promoted to:
- `go-tony/kindedpath` (top-level package)
- `go-tony/ir/kindedpath` (under ir package)

The package is designed to be independent of storage-specific code to facilitate this promotion.

## Token Streaming Integration

### Overview

The storage system leverages token streaming (`TokenSource` and `TokenSink` from `go-tony/token`) for:
1. **Streaming reads** - Read sub-documents from offsets without loading entire snapshots
2. **Offset tracking** - Track byte positions for inverted index and path indexing
3. **Path detection** - Identify node boundaries during encoding for sub-document indexing
4. **Memory efficiency** - Process large documents incrementally without full memory load

### TokenSource: Streaming Tokenization

**Usage**: Read entries/sub-documents from arbitrary offsets
```go
sectionReader := io.NewSectionReader(file, offset, size)
source := token.NewTokenSource(sectionReader)

// Parse directly from TokenSource (streaming, no token collection)
node, err := parse.ParseFromTokenSource(source)
```

**Benefits**:
- Can read from arbitrary offsets (enables path indexing)
- Processes incrementally (bounded memory usage)
- Works with `io.SectionReader` for partial reads
- **Streaming parse**: Parses as tokens arrive, doesn't collect all tokens first

**Required API**: `parse.ParseFromTokenSource(*token.TokenSource, ...ParseOption) (*ir.Node, error)` - needs to be added to `go-tony/parse` package. **Must parse incrementally** - cannot collect all tokens first (defeats the purpose of streaming).

### TokenSink: Streaming Encoding with Offset Tracking

**Usage**: Write entries with offset tracking for indexing
```go
sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
    // Record offsets for indexing
})
sink.Write(tokens)
```

**Benefits**:
- Tracks absolute byte offsets (`Offset()`)
- Detects node starts via callback (for boundary detection)
- Enables building inverted index during encoding

### Impact

**Critical Capabilities Enabled**:
- ✅ Inverted index sub-document approach (see `inverted_index_subdocuments.md`)
- ✅ Path indexing within snapshots (see `snapshot_path_indexing.md`)
- ✅ Memory-efficient snapshot processing
- ✅ Position-aware writing for compaction

See `token_streaming_impact_analysis.md` for detailed impact analysis.

## Related Documents

- **Implementation Plan**: `docs/implementation_plan.md` - Step-by-step implementation guide
- **Kinded Path Package**: `docs/kindedpath_package.md` - Package documentation
- **Read Algorithm**: `docs/read_algorithm.md` - Detailed read operation specification
- **Token Streaming Impact**: `docs/token_streaming_impact_analysis.md` - Impact analysis of token streaming
- **Inverted Index Design**: `docs/inverted_index_subdocuments.md` - Sub-document indexing approach
- **Path Indexing Design**: `docs/snapshot_path_indexing.md` - Path indexing within snapshots
- **Design Decisions Archive**: `docs/design_decisions_archive.md` - Historical decision-making process
