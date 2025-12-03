# Storage Redesign: Implementation State

> **Note**: This document tracks the current implementation state.
> 
> **Use `DESIGN.md` for the final design reference.**
> **Use `implementation_plan.md` for implementation guidance.**

## Overview

We're redesigning the storage system to fix fundamentally broken concepts, particularly issues with `ReadAt` and compaction. The design has evolved through iterative bottom-up and top-down discussions, converging on a clean, simple design.

## Core Design Decisions

### 1. Storage Layout ✅

**Logs by Level**: One file per level (`level0.log`, `level1.log`, etc.)
- Sequential append-only logs
- LogPosition = byte offset within log file
- Index maps `(path, level)` → `(logPosition, commitRange)`

**Rationale**: Matches hierarchy, efficient sequential reads, simple structure.

### 2. Log Entry Format ✅

**Schema** (defined in `log_entry.go`):
```go
type LogEntry struct {
    Commit    int64      // Commit number (set when appended to log)
    Seq       int64      // Transaction sequence (txSeq)
    Timestamp string     // Timestamp
    Diff      *ir.Node   // Root diff (always at "/") - contains all paths affected by this commit
}
```

**Key Points**:
- ✅ `Path` field **removed** (always root, redundant to store)
- ✅ `Inputs` NOT stored (computed from correctness criteria)
- ✅ `Level` NOT stored (derived from log filename)
- ✅ `Pending` flag removed (can add later if needed)

### 3. Log File Format ✅

**Length-Prefixed Entries**: `[4 bytes: uint32 length (big-endian)][entry data in Tony wire format]`

**Why Length-Prefixed**:
- Simpler random access (read length, read exact bytes)
- Easier recovery (can skip entries without parsing)
- Clearer boundaries (explicit entry boundaries)
- Negligible overhead (~4 bytes per entry, ~4% for typical entries)

**Reading**:
```go
// 1. Read 4 bytes → get length
// 2. Read length bytes → get entry data
// 3. Parse entry data from Tony wire format
```

### 4. Subtree-Inclusive Parent Diffs ✅

**Key Insight**: When writing `a.b`, write diff at root with `{a: {b: <diff>}}` (includes subtree).

**Write Pattern**:
- Always write **ONE entry at root** per commit
- Merge all paths into root diff
- Example: Write `a.b` and `a.b.c` → ONE entry at root with `{a: {b: {y: 2, c: {z: 3}}}}`

**Why Root?**
- Simpler logic (always write at `/`, no need to find common parent)
- Consistent structure (all entries at same path)
- Minimal performance difference (ExtractPath is longer but extraction is still O(depth))

**Benefits**:
- No repetition (one entry instead of multiple)
- Single source of truth (parent diff includes all child changes)
- Efficient (fewer entries in log)

### 5. Index Structure ✅

**LogSegment** (with `LogPosition` and `KindedPath` fields, `RelPath` removed):
```go
type LogSegment struct {
    StartCommit int64
    StartTx     int64
    EndCommit   int64
    EndTx       int64
    KindedPath  string  // "a.b.c" (kinded path - used for querying and extraction; "" for root)
    LogPosition int64   // Byte offset in log file
}
```

**Indexing**:
- Write `a.b` and `a.b.c` → ONE entry at root with `{a: {b: {y: 2, c: {z: 3}}}}`
- Index `a` → `{LogPosition: 200, KindedPath: "a"}`
- Index `a.b` → `{LogPosition: 200, KindedPath: "a.b"}`
- Index `a.b.c` → `{LogPosition: 200, KindedPath: "a.b.c"}`

**KindedPath**: Kinded path string (e.g., "a.b.c") - encodes node kinds in syntax.

### 6. Write Operations ✅

**In-Memory Buffer Only**:
- Accumulate entries in in-memory buffer during transaction
- Atomically append to log on commit
- **Design constraint**: Transactions must fit in memory
- **Crash handling**: Client sees failure and retries (no transaction resumption)

**Write Flow**:
1. **During transaction**: Participants add patches independently (no contention)
2. **At commit** (single-threaded, serialized by commit):
   - Merge all patches into root diff
   - Write ONE entry at root with merged diff
   - Index all paths to root entry with KindedPath

**Write Contention**: ✅ No additional contention
- Within transaction: No contention (participants add independently)
- At commit: Single-threaded merge (commits already serialized)
- Root path: Most work, but still single-threaded at commit time

### 7. Read Operations ✅

**Read Flow**:
1. Query index for path at commit N
2. Get segments with `LogPosition` and `KindedPath`
3. Read entry at `LogPosition` (length-prefixed)
4. Extract path using `KindedPath` (if not empty, extract from root diff)
5. Apply entries in commit order

**Example**:
```go
// Read a.b.c at commit 2
segment := index.Query("a.b.c", commit=2)
// Returns: {LogPosition: 200, KindedPath: "a.b.c"}

entry := ReadEntryAt(logFile, 200)
// Entry.Diff = {a: {b: {y: 2, c: {z: 3}}}}

childDiff := kindedpath.Get(entry.Diff, segment.KindedPath)  // Extract "a.b.c"
// Result: {z: 3}
```

### 8. Index Persistence ✅

**Hybrid Approach**: Persist index with max commit number
- **Normal startup**: Load persisted index, scan logs from `MaxCommit + 1` forward (incremental rebuild)
- **Corruption/recovery**: Full rebuild from logs if index missing/corrupted
- **Rationale**: Fast startup (most of the time), logs are source of truth

**IndexMetadata**:
```go
type IndexMetadata struct {
    MaxCommit int64 // Highest commit number in the index
}
```

### 9. Path Representation ✅

**Storage**: Path removed from `LogEntry` (always root)
- Paths are in diff structure (traverse to find paths)
- Index uses `KindedPath` (the path being queried and extracted)
- `ExtractPath` is relative path from root (e.g., "a.b.c")

**Path Operations** (still needed):
- Parse kinded path string → `PathSegment` (for operations)
- Extract paths from diff structure (for recovery)

## What's Complete ✅

1. ✅ **Schema**: `LogEntry` struct defined (no Path field, always root)
2. ✅ **Storage Layout**: Logs by level, byte offsets
3. ✅ **Log File Format**: Length-prefixed entries
4. ✅ **Subtree-Inclusive Diffs**: Always write at root, merge all paths
5. ✅ **Index Structure**: `LogSegment` with `LogPosition` and `ExtractPath`
6. ✅ **Write Operations**: In-memory buffer → atomic append
7. ✅ **Read Operations**: Query index → read at offset → extract path
8. ✅ **Index Persistence**: Hybrid approach with max commit
9. ✅ **Write Contention**: No additional contention (commits serialized)
10. ✅ **Level Switching**: Same entry supports multiple read levels via `KindedPath`
11. ✅ **Token Streaming API**: `TokenSource` and `TokenSink` available from `go-tony/token`
    - Enables streaming reads for large entries/sub-documents
    - Enables offset tracking for path indexing
    - Enables inverted index building during encoding
    - Enables memory-efficient snapshot processing

## What's Missing ❌

### Critical Missing Components

1. **Log File I/O Operations**:
   - `OpenLogFile(level int) (*LogFile, error)`
   - `AppendEntry(entry *LogEntry) (int64, error)` - write length prefix + entry
   - `ReadEntryAt(offset int64) (*LogEntry, error)` - read length prefix + entry
   - `AppendEntryWithOffsets(entry *LogEntry) (int64, map[string]int64, error)` - write with offset tracking (using `TokenSink`)
   - `ReadSubDocumentAt(offset int64, size int64) (*ir.Node, error)` - streaming read (using `TokenSource`)
   - File locking/concurrent access handling

2. **Entry Serialization/Deserialization**:
   - `LogEntryToTony(entry *LogEntry) ([]byte, error)`
   - `LogEntryFromTony(data []byte) (*LogEntry, error)`
   - Integration with Tony wire format

3. **Path Operations**:
   - `ParseKindedPath(pathStr string) (*PathSegment, error)`
   - Extract paths from diff structure (for recovery)
   - `Diff.GetPath(extractPath string) (*ir.Node, error)` - extract nested path

4. **Transaction Buffer Operations**:
   - `TransactionBuffer` struct
   - `AddPatch(path string, diff *ir.Node)`
   - `MergeToRoot() *ir.Node` - merge all patches into root diff
   - `Serialize() ([]byte, error)` - serialize for append

5. **Index Integration**:
   - Update `LogSegment` to include `LogPosition` and `KindedPath`
   - `Index.Add(path string, logPosition int64, kindedPath string)`
   - `Index.Query(path string, commit int64) []*LogSegment`
   - Extract paths from diff structure when rebuilding index (using `kindedpath.ExtractAll`)

6. **Recovery/Index Rebuild**:
   - Scan log files sequentially
   - Read length-prefixed entries
   - Traverse diff structure to find all paths
   - Index each path with ExtractPath

### Design Questions Still Open

1. **Compaction**: How does compaction work with new layout?
   - How to compact entries from level L-1 to level L?
   - How to handle ExtractPath in compacted segments?

2. **Error Handling**: How to handle corrupted entries?
   - Partial writes (length prefix written but entry data truncated)
   - Corrupted entry data (parser fails)
   - Recovery strategy (skip entry? mark as corrupted?)

3. **File Management**: How to handle log file growth?
   - Truncate/compact old entries?
   - Keep forever (append-only)?
   - Rotation strategy?

## Key Simplifications

1. ✅ **No Path field** - always root, redundant to store
2. ✅ **In-memory buffer only** - no temp files, transactions must fit in memory
3. ✅ **Length-prefixed entries** - simpler random access, easier recovery
4. ✅ **Always write at root** - simpler logic, consistent structure
5. ✅ **Subtree-inclusive diffs** - no repetition, single source of truth
6. ✅ **Index persistence with max commit** - fast startup, incremental rebuild

## Next Steps

1. **Implement Log File I/O**:
   - `LogFile` struct with `AppendEntry` and `ReadEntryAt`
   - Length-prefixed read/write operations
   - File locking/concurrent access

2. **Implement Entry Serialization**:
   - `LogEntryToTony` and `LogEntryFromTony`
   - Integration with Tony wire format

3. **Implement Path Operations**:
   - `ParseKindedPath` (if needed for operations)
   - `Diff.GetPath` - extract nested path from root diff

4. **Implement Transaction Buffer**:
   - Accumulate patches
   - Merge to root diff at commit time
   - Serialize for append

5. **Update Index Structure**:
   - Add `LogPosition` and `ExtractPath` to `LogSegment`
   - Update index operations to use new fields

6. **Design Compaction**:
   - How to compact entries with new layout
   - How to handle ExtractPath in compacted segments

7. **Design Recovery**:
   - Scan log files
   - Rebuild index from entries
   - Handle corrupted entries

8. **Token Streaming Integration**:
   - Integrate `TokenSource` for streaming reads of large entries
   - Integrate `TokenSink` for offset tracking during writes
   - Build inverted index using `TokenSink` callbacks
   - Implement path indexing using offset tracking

## Design Principles

1. **Simplicity**: Prefer simple solutions over complex ones
2. **Consistency**: Consistent structure (all entries at root)
3. **Efficiency**: Minimize storage and read overhead
4. **Correctness**: Logs are source of truth, index is optimization
5. **Recoverability**: Can rebuild index from logs

## Files Updated

- ✅ `log_entry.go` - LogEntry schema (Path field removed)
- ✅ `docs/redesign_outline.md` - Main design document
- ✅ `docs/redesign_summary.md` - Summary document
- ✅ `docs/parent_diff_indexing.md` - Subtree-inclusive diff design
- ✅ `docs/parent_diff_write_contention.md` - Write contention analysis
- ✅ `docs/single_entry_per_path.md` - Why merge to root
- ✅ `docs/extract_key_vs_path.md` - ExtractPath vs ExtractKey
- ✅ `docs/remove_path_field.md` - Why remove Path field
- ✅ `docs/length_prefix_analysis.md` - Length-prefixed format analysis
- ✅ `docs/bottom_up_critique.md` - Missing bottom-level components
- ✅ `docs/read_path_sanity_check.md` - Read operation sanity check
- ✅ `index/persist.go` - Index persistence with max commit

## Current State: Ready for Implementation

We have a complete, consistent design with:
- ✅ Clear schema (LogEntry without Path field)
- ✅ Clear format (length-prefixed entries)
- ✅ Clear write flow (in-memory buffer → root diff → append)
- ✅ Clear read flow (query index → read at offset → extract path)
- ✅ Clear index structure (LogSegment with LogPosition and ExtractPath)
- ✅ **Token streaming API available** (enables critical optimizations)

**Token Streaming Enables**:
- ✅ Streaming reads for large entries/sub-documents (memory-efficient)
- ✅ Offset tracking for path indexing (1000x I/O reduction for controller use case)
- ✅ Inverted index building during encoding (sub-document indexing)
- ✅ Memory-efficient snapshot processing (can handle 1GB+ snapshots)

**Next**: Implement the missing bottom-level components (I/O, serialization, path operations) with token streaming integration.
