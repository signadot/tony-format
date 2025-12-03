# Storage Redesign: Current State Summary

> **Note**: This document has been superseded by `DESIGN.md` and `implementation_plan.md`.
> This file is kept for historical reference but should not be used as the primary design reference.
> 
> **Use `DESIGN.md` for the final design reference.**
> **Use `implementation_plan.md` for implementation guidance.**
> 
> **Note**: This document uses old path format ("/a/b"). The final design uses kinded paths ("a.b") throughout.

## Core Problems Being Solved

1. **ReadAt Doesn't Actually Read "At"**
   - Current implementation blindly applies diffs sequentially without respecting hierarchy
   - Cannot correctly reconstruct hierarchical state at a given commit

2. **Storage Layout Doesn't Match Hierarchy**
   - Files scattered by virtual path, assumes local path locality
   - Inefficient for hierarchy queries

3. **Data Structures Assume Locality**
   - Organized around local virtual paths, not hierarchy
   - Makes hierarchy operations complex and error-prone

## What We're Keeping

- **Index Structure**: Hierarchical index design works well, keep as-is
- **Compaction Correctness Criteria**:
  - Level 0: Always correct at any commit N
  - Level 1: Correct at commit N if N % divisor == 0
  - Level 2: Correct at commit N if N % divisor^2 == 0
  - Level L: Correct at commit N if N % divisor^L == 0

## Key Design Decisions

### 1. Storage Layout
- **Logs by Level**: One file per level (`level0.log`, `level1.log`, etc.)
- **Log Format**: Tony wire format, sequential append-only
- **LogPosition**: Byte offset within log file
- **Index**: Persisted with max commit number, maps `(path, level)` → `(logPosition, commitRange)`
- **Recovery**: 
  - **Normal startup**: Load persisted index, scan logs from `MaxCommit + 1` forward (incremental rebuild)
  - **Corruption/recovery**: Full rebuild from logs if index missing/corrupted
- **Index Persistence**: Persisted for performance (fast startup), not correctness (logs are source of truth)

### 2. Kinded Paths
- **Path Syntax Encodes Kind**:
  - `a.b` → Object accessed via `.b`
  - `a[0]` → Dense Array accessed via `[0]`
  - `a{0}` → Sparse Array accessed via `{0}`
- **Kind Storage**: Derive kind from path syntax (no separate field needed)
- **Rationale**: Solves "how do we know kind when document is empty?" - path tells us

### 3. Subtree-Inclusive Parent Diffs
- **Store Subtree-Inclusive Parent Diffs**: When writing diff at `/a/b`, write diff at `/a` with `{b: <diff>}` (includes subtree)
- **Index Both Paths**: Index maps both `/a` and `/a/b` to same log entry
  - `/a` → `{LogPosition: 200, ExtractKey: ""}` (read entire diff)
  - `/a/b` → `{LogPosition: 200, ExtractKey: "b"}` (extract `b` from parent diff)
- **Multiple Children**: Merge into single parent diff (e.g., `/a/b` and `/a/c` → `/a` with `{b: {...}, c: {...}}`)
- **Level Switching**: Same entry supports reading at parent and child levels via `ExtractKey`
- **Write Contention**: ✅ No additional contention (commits serialized, merge at commit time)
- **Computation**: Can compute directly from child diff if we know kind of every node in path
- **Kind Source**: Parse kinded path to get kinds at each level
- **Result**: Reads can extract children from parent diffs, or read parent diffs directly

### 4. Write Serialization
- **No Concurrent Writes**: Serialized by commit number (via sequencer)
- **Multi-Path Transactions**: Each commit has a set of paths updated (multi-participant transaction pattern)
- **Atomicity**: All diffs for a commit written atomically

### 5. Read Operations
- **Query Index**: Get all segments for path at commit N
- **Determine Starting Level**: Use compaction alignment (N % divisor^L == 0)
- **Read Base State**: If starting level > 0, read compacted segment
- **Filter Segments**: Remove segments covered by compacted segment
- **Sort by LogPosition**: Sort byte offsets for sequential reads
- **Read Sequentially**: Read from log file at sorted offsets, apply diffs in commit order
- **Efficiency**: Sequential reads minimize seeks (better than scattered layout)

## Current State

### Completed Design Decisions
- ✅ Storage layout (logs by level, byte offsets)
- ✅ Kinded paths (derive kind from path syntax)
- ✅ **Subtree-inclusive parent diffs** (write at parent path, index both parent and child)
- ✅ **Index structure** (add `LogPosition` and `ExtractKey` to `LogSegment`)
- ✅ **Write contention** (no additional contention, commits serialized)
- ✅ **Level switching** (same entry supports multiple read levels)
- ✅ Read algorithm (sanity checked, efficient)
- ✅ **Log Entry Schema**: Concrete `LogEntry` struct defined (`log_entry.go`)

### Missing Bottom-Level Components (Critical)

**We have the schema (top-down) but need the operations (bottom-up)**:

1. ❌ **Log File I/O Operations**
   - How to open/append/read log files (`level0.log`, `level1.log`, etc.)
   - ✅ **Reading**: Use `io.ReaderAt` + `io.NewSectionReader` (no explicit seek needed!)
   - How to handle file locking/concurrent access

2. ❌ **Entry Serialization/Deserialization**
   - How to serialize `LogEntry` to Tony wire format bytes
   - How to deserialize from reader (`LogEntryFromTony(reader io.Reader) (*LogEntry, int64, error)`)
   - How to handle parsing errors/partial writes

3. ❌ **Path Operations**
   - ✅ **Path Storage**: Store as `string` in log (simpler, more compact)
   - How to parse kinded path string → `PathSegment` (`ParseKindedPath()`)
   - How to serialize `PathSegment` → string (`PathSegment.String()`)
   - How to compare/extract parent paths

4. ❌ **Transaction Buffer Operations**
   - How to accumulate entries in buffer
   - How to serialize buffer to bytes
   - How to append buffer to log atomically

5. ❌ **Index Integration**
   - How to add entries to index with log positions
   - How to query index for log positions
   - How to update `LogSegment` with `LogPosition` and `ExtractKey` fields
   - How to index both parent and child paths to same entry (different `ExtractKey`)

**See `bottom_up_critique.md` for detailed analysis and priority order**

**Sanity Check**: See `read_path_sanity_check.md` - verified that bottom-level components support higher-level read operations:
- ✅ Can query index for log positions
- ✅ Can read at byte offsets using `io.ReaderAt`
- ✅ Can parse entries from readers
- ✅ Can parse paths when needed
- ✅ Can sort and apply diffs in commit order

### In Progress
- 🔄 Write operations (how to write diffs, atomicity, partial writes)
- 🔄 Compaction (how compaction works with new layout)
- 🔄 Recovery (index rebuild from logs, handling corrupt entries)

### Open Questions
- **Log File Format**:
  - ✅ Format: Length-prefixed entries (`[4 bytes: length][Tony wire format entry]`)
  - ✅ Reading: Read 4 bytes for length, read exact bytes, parse Tony wire format
  - ✅ Why: Simpler random access, easier recovery, clearer boundaries, negligible overhead
  - ✅ **Schema Defined**: `LogEntry` struct in `log_entry.go` with `//tony:schemagen=log-entry`
    - Fields: `Commit`, `Seq`, `Timestamp`, `Diff`
    - `Path` field removed (always root "/", redundant to store)
    - `Diff`: Root diff (always at "/") - contains all paths affected by this commit
    - `Inputs` NOT stored (computed from correctness criteria)
    - `Level` NOT stored (derived from log filename)
    - `Pending` flag removed (can add later if needed)
    - **Write Timing**: In-memory buffer only, atomically append to log on commit
      - **Design constraint**: Transactions must fit in memory
      - **Crash handling**: Client sees failure and retries (no transaction resumption)
    - **Recovery**: Traverse diff structure to find all paths when rebuilding index
  - ❓ How do we handle partial writes/crashes? (parser will fail, need recovery strategy)
- **Write Operations**:
  - ✅ **Multiple children in same commit**: Merge into single parent diff (e.g., `/a/b` and `/a/c` → `/a` with `{b: {...}, c: {...}}`)
  - ✅ **Write contention**: No additional contention (commits serialized, merge at commit time in transaction buffer)
  - ✅ **Level switching**: Same entry supports reading at parent and child levels via `ExtractKey`
  - How to ensure atomicity for multi-path transactions? (accumulate in buffer, append atomically)
  - How to handle pending diffs in log format? (removed for now, can add later if needed)
- **Compaction**:
  - How does compaction work with new layout?
  - How to handle log file growth (truncate/compact or keep forever)?

## Next Steps

1. ✅ **Define log file format** (entry structure, recursive path, reading at offsets)
2. Complete write operations design (in-memory buffer → atomic append)
3. Design compaction with new layout
4. Design recovery/index rebuild (scan logs, rebuild in-memory index)
5. Implementation plan

## Key Simplifications

1. ✅ **No `Pending` flag** - removed from schema
2. ✅ **In-memory buffer only** - no temp files, transactions must fit in memory
3. ✅ **Index persistence with max commit** - fast startup (load index) + incremental rebuild (scan from max commit forward)
