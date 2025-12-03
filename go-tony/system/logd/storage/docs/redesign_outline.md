# Storage Redesign: Complete Do-Over

## Core Problems (Root Causes)

### 1. ReadAt Doesn't Actually Read "At"
- **Current behavior**: Blindly applies diffs sequentially without respecting hierarchy levels
- **Problem**: A diff at `/a/b` and a diff at `/a` are treated the same way
- **Impact**: Cannot correctly reconstruct hierarchical state at a given commit

### 2. Storage Layout Doesn't Match Hierarchy
- **Current**: Files scattered by virtual path, assumes local path locality
- **Problem**: Filesystem structure doesn't reflect document hierarchy
- **Impact**: Hard to reason about hierarchy, inefficient queries

### 3. Data Structures Assume Locality
- **Current**: Organized around local virtual paths
- **Problem**: Doesn't model hierarchy properly
- **Impact**: Hierarchy operations are complex and error-prone

### 4. Architecture Doesn't Follow Modern DB Patterns
- **Current**: Log stores references to diffs (backwards)
- **Should be**: Store diffs, reference them (like modern databases)
- **Impact**: Inefficient, hard to reason about

## What to Keep

### Index Structure
- Current index design is sound
- Hierarchical organization works

### Compaction Correctness Criteria
- Level 0: Always correct at any commit N
- Level 1: Correct at commit N if N % divisor == 0
- Level 2: Correct at commit N if N % divisor^2 == 0
- Level L: Correct at commit N if N % divisor^L == 0

## Redesign Goals

### 1. Correct Hierarchy Construction
- ReadAt must properly construct hierarchy by level
- Diffs at different hierarchy levels must be applied correctly
- Query at commit N must return exact state at that commit

### 2. Hierarchical Storage Layout
- Filesystem structure should mirror document hierarchy
- Storage should be organized by hierarchy, not by local paths

### 3. Modern Database Patterns
- Store diffs directly in log
- Reference diffs (not references to diffs)
- Follow log-structured merge tree principles

### 4. Simplified Data Structures
- Model hierarchy explicitly
- Remove assumptions about locality
- Make hierarchy operations first-class

## Bottom-Up Design (Building Blocks)

### Level 1: Atomic Primitives

#### 1.1 Diff Storage
- **What**: A diff is an `ir.Node` representing changes to a document path
- **Current**: Stored in `DiffFile` with Seq, Path, Timestamp, Diff, Pending flag
- **Questions**:
  - Should diffs be stored by path or by hierarchy level?
  - How do we represent hierarchy in the diff itself?
  - Do we need the Path field if storage is hierarchical?

#### 1.2 Commit Sequence
- **What**: Monotonically increasing commit numbers
- **Current**: Managed by `seq.Seq` with Commit and TxSeq counters
- **Keep**: The sequencer concept, commit allocation
- **Questions**:
  - Does commit allocation need to change?
  - How do commits relate to hierarchy levels?

#### 1.3 Log Segment
- **What**: A range of commits covered by a diff file
- **Current**: `index.LogSegment` with StartCommit, EndCommit, StartTx, EndTx, RelPath
- **Keep**: The segment concept, commit ranges
- **Questions**:
  - How do segments relate to hierarchy levels?
  - Does RelPath need to change for hierarchical storage?

### Level 2: Hierarchy Representation

#### 2.1 Path Hierarchy Semantics
- **Key Insight**: When a diff is written at `/a/b`, semantically BOTH `/a/b` AND `/a` have changed
- **Reason**: Parent contains child, so child changes affect parent
- **Implication**: We can compute diff for `/a` when `/a/b` changes (or when `/a/b` and `/a/c` change)
- **Model**: 
  - Diffs are written at specific paths (e.g., `/a/b`)
  - Parent diffs can be computed from child changes
  - Don't need to read all child diffs - can compute parent diff from child diffs

#### 2.2 Path Hierarchy Model
- **What**: How do we represent `/a/b/c` as a hierarchy?
- **Current**: Flat path strings, scattered storage
- **Need**: Explicit hierarchy model with parent/child relationships
- **Structure**: Tree where each node knows its parent and children
- **Questions**:
  - How do we efficiently find all children of a path?
  - How do we represent root vs non-root paths?
  - Do we need explicit parent pointers or derive from path?

#### 2.3 Diff Application Order
- **What**: Order in which diffs must be applied to construct hierarchy
- **Current**: Sequential by commit (WRONG - ignores hierarchy)
- **Need**: Apply diffs respecting hierarchy relationships
- **Key**: When reading `/a` at commit N:
  1. Start with base state (null or compacted segment)
  2. Apply all diffs directly to `/a` (commits <= N)
  3. For children `/a/*`: compute their state and incorporate into `/a`
     - Can compute parent diff from child changes
     - Or read child states and merge into parent
- **Questions**:
  - Do we compute parent diffs on-the-fly from child diffs?
  - Or do we store computed parent diffs (during write or compaction)?
  - How do we efficiently compute parent diff from multiple child diffs?

#### 2.4 State Construction Algorithm
- **What**: How to build document state from diffs
- **Current**: Start with null, apply diffs sequentially (WRONG)
- **Need**: Construct hierarchy respecting parent/child relationships
- **Decision**: Store subtree-inclusive parent diffs when writing
- **Algorithm**:
  - **Write**: When writing diff at `/a/b`:
    1. Accumulate patch in transaction buffer: `{Path: "/a/b", Diff: {y: 2}}`
    2. At commit time: Group all patches by parent path
    3. Compute parent diff for `/a`: `{b: {y: 2}}` (subtree-inclusive)
    4. Write single entry at `/a`: `{Path: "/a", Diff: {b: {y: 2}}}`
    5. Index both `/a` and `/a/b` to same entry:
       - `/a` → `{LogPosition: 200, ExtractKey: ""}`
       - `/a/b` → `{LogPosition: 200, ExtractKey: "b"}`
  - **Multiple children in same commit**: Merge into single parent diff
    - Write `/a/b` and `/a/c` → parent diff: `{b: {...}, c: {...}}`
    - Index both children to same entry with different `ExtractKey`
  - **Read**: Read `/a` at commit N:
    1. Get base state (null or compacted segment)
    2. Query index for `/a` → get entries with `ExtractKey: ""`
    3. Read entries, apply diffs in commit order
    4. Result: correct `/a` state (includes all child changes)
  - **Read**: Read `/a/b` at commit N:
    1. Query index for `/a/b` → get entries with `ExtractKey: "b"`
    2. Read entry at `/a`, extract `b` part from parent diff
    3. Apply in commit order if multiple entries
    4. Result: correct `/a/b` state
- **Parent Diff Computation**:
  - Compute directly from child diff using node kind information
  - Know kind of every node in path → can compute parent diff
  - Don't need full before/after states
- **Kind Source**: Use Kinded Paths (path syntax encodes kind)
  - **Kinded Path Syntax**:
    - `a.b` → `a` is Object, accessed via `.b`
    - `a[0]` → `a` is Dense Array, accessed via `[0]`
    - `a{0}` → `a` is Sparse Array, accessed via `{0}`
  - **Example**: Empty document, patch at `a.b.c[0][42].d` with value "hello"
    - Path syntax tells us: `a` is Object, `b` is Object, `c` is Dense Array, `0` element is Dense Array, `42` element is Object, `d` is the target
    - No need to infer or read current state - kind is in the path!
  - **When writing diff**:
    1. Parse kinded path to extract kinds at each level
    2. Write diff with path (kinds encoded in path syntax)
    3. Compute parent diff using kinds from path
    4. Write computed parent diff with its kinded path
  - **Decision**: Derive kind from kinded path syntax (no separate storage needed)
    - Parse path to extract kinds at each level
    - No redundancy, but need to parse path (acceptable trade-off)
- **Write Contention**: ✅ No additional contention
  - Commits are serialized, so merge happens single-threaded at commit time
  - Accumulate patches in transaction buffer (no contention during transaction)
  - Compute parent diffs at commit time (single-threaded merge)
  - Root path gets most work, but still single-threaded
  - See `parent_diff_write_contention.md` for detailed analysis
- **Level Switching**: ✅ Supported via `ExtractKey`
  - Same entry supports reading at parent and child levels
  - Read `/a`: `ExtractKey: ""` → get full diff `{b: {...}, c: {...}}`
  - Read `/a/b`: `ExtractKey: "b"` → extract `b` from same entry
  - Easy to switch between levels when reading
- **Multiple Children**: ✅ Merged into single parent diff
  - Write `/a/b` and `/a/c` → single entry at `/a` with `{b: {...}, c: {...}}`
  - Index maps both children to same entry with different `ExtractKey`

### Level 3: Storage Layout

#### 3.1 Current Layout (Problems)
- **Current**: Scattered by virtual path
  - Diff at `/a/b` stored in `/root/paths/children/a/children/b/filename.diff`
  - Each path has its own directory
  - Filename includes path: `FormatLogSegment` returns `path.Join(s.RelPath, base)`
- **Problems**:
  - To find all diffs at `/a` and `/a/*`: must scan `/a` dir + recursively scan all child dirs
  - Inefficient for hierarchy queries
  - Doesn't follow modern DB patterns (stores refs to diffs, not diffs themselves)

#### 3.2 Storage Layout Decision
- **Design**: Log(s) store diffs sequentially, index maps paths to log positions
- **Structure**:
```
/root/
  level0.log       # Single file for level 0, Tony wire format
  level1.log       # Single file for level 1 (compacted)
  level2.log       # Single file for level 2 (compacted)
  ...
```
- **Key**: 
  - 1 file per level (not multiple files)
  - Log contents: Tony wire format
  - LogPosition: byte offset within the log file
  - Diffs appended sequentially to log file
- **Index**: Maps `(path, level)` → `(logPosition, commitRange)`

#### 3.2.1 Log File Format
- **Format**: Length-prefixed entries written sequentially
- **Entry Structure**: `[4 bytes: uint32 length (big-endian)][entry data in Tony wire format]`
  - Length prefix: 4-byte uint32 (big-endian) indicating entry data size
  - Entry data: Tony wire format serialization of `LogEntry`
  - Entries are variable-size (depends on diff content)
  - **Why length-prefixed**: Simpler random access, easier recovery, clearer boundaries, negligible overhead (~4 bytes per entry)

#### 3.2.2 Log Entry Schema (TO BE DEFINED)
- **Current DiffFile structure**:
  ```go
  type DiffFile struct {
      Seq       int64
      Path      string
      Timestamp string
      Diff      *ir.Node
      Pending   bool
      Inputs    int  // compaction metadata
  }
  ```
- **Log Entry Schema** (defined in `log_entry.go`):
  ```go
  //tony:schemagen=path-segment
  type PathSegment struct {
      Key  string       // The key/index (Tony string representation)
      Kind ir.Type      // ObjectType, ArrayType, SparseArrayType
      Next *PathSegment // Next segment (nil for leaf)
  }
  
  //tony:schemagen=log-entry
  type LogEntry struct {
      Commit    int64      // Commit number (set when appended to log)
      Seq       int64      // Transaction sequence (txSeq)
      Timestamp string     // Timestamp
      Diff      *ir.Node   // Root diff (always at "/") - contains all paths affected by this commit
  }
  ```
  - Schema will be generated to `schema_gen.tony` by `tony-codegen` tool
  - **Path Field Removed**: All entries are written at root "/" - Path field is not stored as it's always "/"
  - **Subtree-Inclusive Diffs**: When writing `/a/b`, merge into root diff at `/` with `{a: {b: <diff>}}`
  - **Recovery**: Traverse diff structure to find all paths when rebuilding index
  - **Write Timing**: Accumulate entries in in-memory buffer, then atomically append to log on commit
    - Allows validation before commit
    - Easy rollback on abort (just discard buffer)
    - Atomic append operation ensures consistency
    - **Design constraint**: Transactions must fit in memory (simplifies implementation, allows focus on bigger picture)
    - **Crash handling**: Client sees failure and retries (no transaction resumption needed)
  
- **Field Decisions**:
  1. **Commit**: ✅ Stored in entry (needed for recovery to rebuild index)
  2. **Pending**: ✅ Stored in entry (handles pending diffs in single log file)
  3. **Inputs**: ❌ **NOT stored** - computed from correctness criteria:
     - For entry at level L, commit C: inputs are entries from level L-1 in range [prevAlign..C)
     - Example: Level 1, commit 16 (2nd entry, index 1) has inputs from Level 0 covering commits [16..32)
     - Formula: `prevAlign = (C / divisor^L) * divisor^L`, `nextAlign = prevAlign + divisor^L`
     - Query level L-1 for entries with commits in [prevAlign..nextAlign), count them
  4. **Level**: ❌ **NOT stored** - derived from log filename (`level0.log` = level 0, `level1.log` = level 1)
  5. **Path**: ✅ Stored as kinded path (syntax encodes kind, no separate `Kind` field)

#### 3.3 Diff File Format & Write Serialization
- **Current**: `DiffFile` contains `Seq`, `Path`, `Timestamp`, `Diff`, `Pending`
- **New**: `DiffFile` contains `Seq`, `Path` (kinded path), `Timestamp`, `Diff`, `Pending`
  - No separate `Kind` field - kind is derived from kinded path syntax
- **Path Format**: Use Kinded Paths (path syntax encodes kind)
  - `a.b` → Object accessed via `.b`
  - `a[0]` → Dense Array accessed via `[0]`
  - `a{0}` → Sparse Array accessed via `{0}`
- **Kind Storage**: Derive kind from kinded path syntax (no redundancy, parse path)
  - Parse kinded path to extract kinds at each level
  - No need to store kind separately - it's encoded in path syntax
  - Similar to git storing file permissions in tree node objects (but encoded in path, not separate field)
  - Kind is `ir.Type` (ObjectType, ArrayType, SparseArrayType, etc.)
  - Needed to compute parent diffs directly from child diffs
- **Write Serialization**: 
  - No concurrent writes to different paths
  - Serialized by commit number (via sequencer)
  - Each commit has a set of paths updated (multi-participant transaction pattern)
  - All diffs for a commit written atomically
- **Diff Format in Log**:
  - Store `Path` (kinded path), `Seq`, `Timestamp`, `Diff`, `Pending` in log entry
  - Path is needed for recovery to rebuild index
  - Kind is derived from path syntax (no separate field needed)
  - Log entry is Tony wire format of `DiffFile` structure
- **Rationale**: 
  - Index maps path to log position, but path must be in log for recovery
  - Kind must be in log to compute parent diffs without reading current state

#### 3.4 Log File Structure
- **Current**: Filename encodes path, commit, tx, level: `path/commit-tx-level.diff`
- **New**: Single file per level: `level0.log`, `level1.log`, etc.
- **Format**: Tony wire format, diffs appended sequentially
- **LogPosition**: Byte offset within the log file
- **Index stores**: `(path, level)` → `(logPosition, startCommit, endCommit, startTx, endTx)`
- **Rationale**: Sequential append-only log, all metadata in index

#### 3.5 Index Integration
- **Keep**: Index structure (hierarchical, works well) - **persisted with max commit number**
- **Change**: Index maps paths to log positions (byte offsets), not filesystem paths
- **Index entry**: `LogSegment` with `RelPath`, `StartCommit`, `EndCommit`, `StartTx`, `EndTx` + `LogPosition` (new field = byte offset) + `KindedPath` (new field for kinded path extraction)
- **Subtree-Inclusive Indexing**: 
  - When writing `/a/b`, write entry at `/` with `{a: {b: <diff>}}`
  - Index `/a` → `{LogPosition: 200, KindedPath: "a"}` (extract "a" from root diff)
  - Index `/a/b` → `{LogPosition: 200, KindedPath: "a.b"}` (extract "a.b" from root diff)
  - Same entry supports reading at parent and child levels
- **Multiple Children**: Merge into single root diff, index all paths to same entry with different `KindedPath`
- **Query**: "All diffs at /a and /a/*" → index lookup returns log positions
- **Implementation**: 
  - Index lookup returns `LogSegment` entries
  - Each entry has `LogPosition` (byte offset) pointing into `level{N}.log` file
  - Read diff from log file at that byte offset
- **Persistence**: Index persisted on shutdown with max commit number
  - **IndexMetadata**: Contains `MaxCommit` (highest commit number in index)
  - **Normal shutdown**: Persist index + metadata
  - **Normal startup**: Load index, scan logs from `MaxCommit + 1` forward (incremental rebuild)
  - **Corruption/recovery**: If index missing/corrupted, full rebuild from logs
- **Recovery**: Index reconstructs itself from logs (full or incremental)
  - **Full rebuild**: Scan all `level{N}.log` files sequentially (if index missing/corrupted)
  - **Incremental rebuild**: Scan logs from `MaxCommit + 1` forward (normal case)
  - Parse Tony wire format entries (each entry is a `LogEntry` with `Path`)
  - Track byte offset as we read (this becomes `LogPosition`)
  - For each entry, create index entry: `(path, level)` → `(logPosition, commitRange)`
  - Build hierarchical index structure from path entries
  - **Rationale**: Index is just a cache/optimization - logs are the source of truth
  - **Performance**: Fast startup (load persisted index) + incremental updates (only scan new entries)

### Level 4: Read Operations

#### 4.1 Read at Commit N - Sanity Check

**Goal**: Read `/a` at commit N, correctly constructing hierarchy

**Step 1: Query Index**
- Query index for all segments where:
  - Path is `/a` OR path starts with `/a/` (children)
  - `StartCommit <= N` (or `EndCommit <= N` for compacted segments)
- Returns: List of `LogSegment` entries, each with `LogPosition` (byte offset)

**Step 2: Determine Starting Level**
- Check compaction alignment: `N % divisor^L == 0` for each level L
- Find highest level L where alignment holds (e.g., if N=8, divisor=2: L=0,1,2,3 all align)
- Start from highest aligned level (e.g., level 3 if N=8)
- If no alignment, start from level 0

**Step 3: Read Base State**
- If starting from level L > 0:
  - Find compacted segment at level L covering commit N
  - Read from `level{L}.log` at `LogPosition`
  - Parse Tony wire format → `DiffFile` → extract `Diff`
  - This is our base state (covers range [StartCommit, EndCommit])
- If starting from level 0:
  - Base state is `null`

**Step 4: Apply Diffs**
- For each level from starting level down to 0:
  - Get all segments at this level for `/a` with commits <= N
  - **Note**: Don't need `/a/*` segments - parent diffs are already computed and stored
  - **Filter**: Remove segments covered by compacted segment (if StartCommit <= seg.StartCommit <= EndCommit)
  - **Keep**: Segments with commits AFTER compacted segment's EndCommit (if any)
  - Sort by commit number (and tx if same commit)
  - For each segment:
    - Read from `level{L}.log` at `LogPosition` (byte offset)
    - Parse Tony wire format → `DiffFile` → extract `Diff` and `Path`
    - Apply diff to state (diff is at `/a`, already computed from children if needed)

**Step 5: Construct Hierarchy**
- After applying all diffs, we have state for `/a`
- To get children, recursively read each child path
- Or: construct entire subtree in one pass by grouping diffs by path depth

**Step 5: Seek Efficiency Analysis**

**Current Layout (Scattered)**:
- Reading `/a` and `/a/*`: 
  - Files in `/paths/children/a/` directory
  - Files in `/paths/children/a/children/b/` directory
  - Files in `/paths/children/a/children/c/` directory
  - etc.
- Each file read requires: open file, read, close (or keep open)
- If 100 diffs across 10 child paths: ~10-100 file operations

**New Layout (Single Log File)**:
- Reading `/a` and `/a/*`:
  - All diffs in `level0.log` at different byte offsets
  - Query index → get list of `LogPosition` values
  - Sort `LogPosition` values (byte offsets) → sequential reads!
  - Read sequentially from sorted offsets: minimal seeks
- If 100 diffs: 
  - Sort 100 byte offsets: O(n log n) but n is small
  - Read sequentially: 1 seek to first offset, then sequential reads
  - Much better than scattered layout!

**Optimization**:
- Sort segments by `LogPosition` (byte offset) before reading
- Read sequentially from log file: seek once, read all entries in order
- This minimizes seeks compared to scattered layout

**Questions/Issues**:
1. **Byte offset reading**: How do we read a specific byte offset from a log file?
   - Seek to offset, read entry
   - Tony wire format is self-describing (knows entry boundaries)
   - After reading one entry, know where next entry starts
   
2. **Compacted segments**:
   - Compacted segment replaces diffs in its range [StartCommit, EndCommit]
   - Recent diffs AFTER EndCommit are kept and applied
   - Old diffs in range are removed (according to config)
   - Filter step: remove segments covered by compacted segment
   
3. **Diff application order**: 
   - Apply by commit order (not path depth)
   - `/a` at commit 2 includes changes from `/a/b` at commit 1
   - So apply `/a/b` diff at commit 1, then `/a` diff at commit 2
   - This correctly constructs hierarchy

4. **Hierarchy construction**:
   - When reading `/a`, we only need diffs at `/a` (parent diffs already computed and stored)
   - Apply all diffs in commit order
   - Result: `/a` state includes all child changes (because parent diffs were computed from children)

---

## Level 1: Write Operations (Back Down)

### 1.1 Writing a Diff
- **Input**: Path, diff, commit number, txSeq
- **Process**:
  1. Open `level0.log` file (append mode)
  2. Get current file size → this is the `LogPosition` (byte offset)
  3. Create `DiffFile` with `Path`, `Seq`, `Timestamp`, `Diff`, `Pending=false`
  4. Serialize to Tony wire format
  5. Append to `level0.log` file
  6. Update index: add `LogSegment` entry with `LogPosition` set to byte offset
- **Questions**:
   - Do we flush/fsync after each write? Or batch?
   - How do we handle partial writes if process crashes?
   - Do we need a write-ahead log for index updates?

### 1.2 Multi-Path Transaction Writes
- **Input**: Set of (path, diff) pairs, single commit number
- **Process**:
  1. Allocate commit number (sequencer)
  2. For each path in transaction:
     - Write diff for path to `level0.log` (get log position)
     - Compute parent diff(s) for all parent paths
     - Write computed parent diff(s) to `level0.log` (same commit)
     - Update index entries for path and parent paths
  3. All writes are for same commit number
  4. All index updates reference same commit number
- **Example**: Write diff at `/a/b/c`:
  - Write diff for `/a/b/c`
  - Compute parent diff for `/a/b` from `/a/b/c` diff
  - Compute parent diff for `/a` from `/a/b` diff (or from `/a/b/c` directly?)
  - Write all three diffs to log (same commit)
  - Update index for all three paths
- **Questions**:
  - How do we compute parent diff? (tony.Diff operation on before/after states?)
  - If multiple children change in same commit, how do we combine their parent diffs?
  - Atomicity: If one write fails, what happens?

### 1.3 Pending Diffs
- **Current**: `.pending` files that get renamed to `.diff`
- **New**: How do we handle pending diffs in log?
  - **Option A**: Write to `level0.log` with `Pending=true`, update index with commit=0
  - **Option B**: Separate `level0.pending.log` file
  - **Option C**: Write to log, mark in index as pending, commit updates index entry
- **Question**: Which approach?

### 1.4 Log File Management
- **Append-only**: Log files grow forever
- **Compaction**: Old diffs removed from index but remain in log
- **Questions**:
   - Do we ever truncate/compact log files? Or keep forever?
   - How do we handle very large log files?
   - Do we need log rotation or splitting?

## Requirements (Given)

1. **Correctness**: ReadAt(path, commit) must return exactly what was written at that commit
2. **Compaction Alignment**: 
   - Level 0: Always correct at any commit N
   - Level 1: Correct at commit N if N % divisor == 0
   - Level 2: Correct at commit N if N % divisor^2 == 0
   - Level L: Correct at commit N if N % divisor^L == 0
3. **Keep**: Index structure (works well)

## Convergence: How Pieces Fit Together

### Write Flow
1. Transaction commits → allocate commit number (sequencer)
2. For each path in transaction:
   - Write diff to `level0.log` (append, get byte offset)
   - Update index: add entry `(path, level=0)` → `(logPosition, commit, commit, txSeq, txSeq)`

### Read Flow
1. Query index for `/a` and `/a/*` at commit N
2. Get list of segments with log positions
3. Determine starting level (compaction alignment)
4. Read compacted segment if starting level > 0
5. Filter out segments covered by compacted segment
6. Sort remaining segments by log position (byte offset)
7. Read sequentially from log file, apply diffs in commit order
8. Result: correct hierarchy state

### Compaction Flow
1. When alignment point reached (N % divisor^L == 0):
   - Read diffs from level L log (using index to find positions)
   - Compute diff from start state to end state
   - Write compacted segment to `level{L+1}.log` (append, get byte offset)
   - Update index: add entry `(path, level=L+1)` → `(logPosition, startCommit, endCommit, startTx, endTx)`
   - Remove old segments from index (according to config)
   - Old diffs remain in log but are no longer referenced

### Key Properties
- **Correctness**: ReadAt returns exactly what was written (hierarchy properly constructed)
- **Compaction Alignment**: Level L correct at commit N if N % divisor^L == 0
- **Efficiency**: Sequential reads from sorted log positions minimize seeks
- **Recovery**: Index rebuilds from logs (scan logs, parse entries, build index)

## Design Process

1. **Bottom-Up Foundation** (current focus)
   - Build from atomic primitives up
   - Answer questions at each level
   - Ensure correctness at each step

2. **Convergence** (above)
   - How building blocks meet requirements
   - Design decisions
   - Trade-offs

3. **Implementation Strategy**
   - Migration path
   - Incremental rollout
   - Testing approach
