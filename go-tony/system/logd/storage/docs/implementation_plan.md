# Storage Redesign: Detailed Implementation Plan

> **Design Reference**: See `DESIGN.md` for the complete design specification.

## Overview

This plan breaks down the implementation into discrete, testable steps. Each step has clear dependencies, deliverables, and verification criteria.

## Prerequisites

- ✅ `LogEntry` struct defined in `log_entry.go` (no Path field)
- ✅ `LogSegment` struct defined in `index/log_segment.go` (with `LogPosition` and `KindedPath`)
- ✅ Index persistence with `MaxCommit` in `index/persist.go`
- ✅ `kindedpath` package structure created
- ✅ **Token streaming API available** (`TokenSource` and `TokenSink` from `go-tony/token`)
- ⚠️ **Streaming parse API needed**: `parse.ParseFromTokenSource(*token.TokenSource, ...ParseOption) (*ir.Node, error)` - needs to be added to `go-tony/parse` package. **Must parse incrementally** - cannot collect all tokens first.
- ✅ Design documents complete (see `DESIGN.md`)

**Note**: Token streaming is a foundational capability that enables:
- Streaming reads for large entries/sub-documents
- Offset tracking for path indexing
- Inverted index building during encoding
- Memory-efficient snapshot processing

**Required API Addition**: The `parse` package needs a public `ParseFromTokenSource` function that:
- Takes `*token.TokenSource` and `...ParseOption`
- **Parses incrementally** as tokens arrive from TokenSource (via `source.Read()`)
- Balances and parses tokens in a streaming fashion
- Returns `*ir.Node`
- **Critical constraint**: Must NOT collect all tokens first - must parse as tokens stream in
- This requires implementing streaming balance/parse logic (see `streaming_parse_design.md` Option A)

## Implementation Steps

---

## Phase 1: Foundation (Serialization & Path Operations)

### Step 1.1: Entry Serialization/Deserialization

**Goal**: Serialize `LogEntry` to/from Tony wire format bytes.

**Files to Create**:
- `log_entry_serialize.go`

**Implementation**:
```go
// Serialize LogEntry to Tony wire format bytes
func (e *LogEntry) ToTony() ([]byte, error)

// Deserialize Tony wire format bytes to LogEntry
func LogEntryFromTony(data []byte) (*LogEntry, error)
```

**Dependencies**: None (uses existing `LogEntry` struct and Tony wire format)

**Test Cases**:
1. ✅ Serialize empty diff → deserialize → compare
2. ✅ Serialize complex diff → deserialize → compare
3. ✅ Serialize with different commits/seqs → deserialize → verify
4. ✅ Round-trip: serialize → deserialize → serialize → compare bytes
5. ✅ Error handling: invalid Tony format → return error

**Verification**:
- [ ] `go test -v ./storage -run TestLogEntrySerialization`
- [ ] All test cases pass
- [ ] Round-trip produces identical bytes

**Checkpoint**: ✅ Can serialize/deserialize `LogEntry` reliably

---

### Step 1.2: Path Extraction from Diff

**Goal**: Extract nested kinded paths from root diff structure.

**Files to Create**:
- `kindedpath/extract.go` (in new kindedpath package)

**Implementation**:
```go
// Extract nested kinded path from root diff
// Example: Get(diff, "a.b.c") extracts {c: {...}} from {a: {b: {c: {...}}}}
func Get(diff *ir.Node, kindedPath string) (*ir.Node, error)

// Extract all paths from root diff (for recovery/indexing)
// Returns map of kinded paths to their values: {"a": {...}, "a.b": {...}, "a.b.c": {...}}
func ExtractAll(diff *ir.Node) (map[string]*ir.Node, error)
```

**Dependencies**: `ir.Node` type (existing)

**Test Cases**:
1. ✅ Extract simple path: `GetPath({a: {b: 1}}, "a.b")` → `{b: 1}`
2. ✅ Extract nested path: `GetPath({a: {b: {c: 1}}}, "a.b.c")` → `{c: 1}`
3. ✅ Extract root: `GetPath({a: 1}, "")` → `{a: 1}`
4. ✅ Extract non-existent path → error
5. ✅ ExtractAllPaths: `{a: {b: 1, c: 2}}` → `{"a": {...}, "a.b": 1, "a.c": 2}`
6. ✅ ExtractAllPaths with nested: `{a: {b: {c: 1}}}` → all paths extracted

**Verification**:
- [ ] `go test -v ./storage -run TestPathExtraction`
- [ ] All test cases pass
- [ ] Handles arrays and sparse arrays correctly

**Checkpoint**: ✅ Can extract paths from root diff structure

---

### Step 1.3: Path Operations

**Goal**: Provide path comparison and manipulation operations.

**Files to Create/Modify**:
- `kindedpath/path.go` (add comparison operations)

**Implementation**:
```go
// Parent returns the parent path segment
func (ps *PathSegment) Parent() *PathSegment

// IsChildOf checks if this path is a child of the given parent
func (ps *PathSegment) IsChildOf(parent *PathSegment) bool

// Compare compares two paths
func (ps *PathSegment) Compare(other *PathSegment) int
```

**Dependencies**: Step 1.1 (PathSegment struct)

**Test Cases**:
1. ✅ Parent("a.b.c") → "a.b"
2. ✅ Parent("a") → ""
3. ✅ IsChildOf("a.b", "a") → true
4. ✅ Compare("a.b", "a.c") → -1 (less than)
5. ✅ Compare("a.b", "a.b") → 0 (equal)

**Verification**:
- [ ] `go test -v ./storage -run TestPathOperations`
- [ ] All test cases pass

**Checkpoint**: ✅ Can manipulate and compare kinded paths

---

### Step 1.4: Diff Application

**Goal**: Implement diff application logic to merge diffs into state.

**Files to Create**:
- `diff_apply.go` (or add to existing file)

**Implementation**:
```go
// Apply a diff to existing state, merging the changes
func applyDiff(state *ir.Node, diff *ir.Node) *ir.Node
```

**Algorithm**:
1. If `state == nil`: return `diff` (first diff initializes state)
2. If `diff == nil`: return `state` (no change)
3. Merge `diff` into `state`:
   - For objects: merge fields (diff fields override state fields)
   - For arrays: merge elements (diff elements override state elements)
   - For deletions: remove fields/elements marked as deleted
   - For insertions: add new fields/elements

**Dependencies**: `ir.Node` type (existing)

**Test Cases**:
1. ✅ ApplyDiff with nil state → returns diff
2. ✅ ApplyDiff with nil diff → returns state
3. ✅ ApplyDiff merges object fields correctly
4. ✅ ApplyDiff merges array elements correctly
5. ✅ ApplyDiff handles deletions correctly
6. ✅ ApplyDiff handles insertions correctly
7. ✅ ApplyDiff handles nested structures correctly

**Verification**:
- [ ] `go test -v ./storage -run TestDiffApply`
- [ ] All test cases pass
- [ ] Merges match expected behavior

**Checkpoint**: ✅ Can apply diffs to state

---

## Phase 2: Log File I/O

### Step 2.1: LogFile Structure

**Goal**: Create `LogFile` struct with basic file operations.

**Files to Create**:
- `log_file.go`

**Implementation**:
```go
type LogFile struct {
    level int
    path  string
    file  *os.File
    mu    sync.RWMutex  // For concurrent access
}

// Open log file for given level
func OpenLogFile(baseDir string, level int) (*LogFile, error)

// Close log file
func (lf *LogFile) Close() error

// Get current file size (for LogPosition)
func (lf *LogFile) Size() (int64, error)
```

**Dependencies**: None

**Test Cases**:
1. ✅ OpenLogFile creates file if doesn't exist
2. ✅ OpenLogFile opens existing file
3. ✅ Close closes file
4. ✅ Size returns current file size
5. ✅ Concurrent opens (read-only) work correctly

**Verification**:
- [ ] `go test -v ./storage -run TestLogFile`
- [ ] File created at correct path (`baseDir/level0.log`)
- [ ] Can open/close multiple times
- [ ] Size returns correct value

**Checkpoint**: ✅ Can create/open/close log files

---

### Step 2.2: Append Entry (Write)

**Goal**: Append length-prefixed entry to log file.

**Files to Modify**:
- `log_file.go`

**Implementation**:
```go
// Append entry to log file, returns LogPosition (byte offset)
func (lf *LogFile) AppendEntry(entry *LogEntry) (int64, error)
```

**Algorithm**:
1. Lock for writing
2. Get current file size (LogPosition)
3. Serialize entry to Tony format (`entry.ToTony()`)
4. Write 4-byte length prefix (big-endian uint32)
5. Write entry data
6. Sync file (ensure durability)
7. Return LogPosition

**Dependencies**: Step 1.1 (entry serialization)

**Test Cases**:
1. ✅ AppendEntry writes length prefix + entry data
2. ✅ AppendEntry returns correct LogPosition
3. ✅ Multiple appends increment LogPosition correctly
4. ✅ File size increases by 4 + entry size
5. ✅ Can read back what was written (manual verification)
6. ✅ Concurrent appends are serialized (lock works)

**Verification**:
- [ ] `go test -v ./storage -run TestLogFileAppend`
- [ ] File contains correct bytes (hex dump)
- [ ] LogPosition increments correctly
- [ ] Multiple entries written sequentially

**Checkpoint**: ✅ Can append entries to log file

---

### Step 2.3: Read Entry at Offset

**Goal**: Read length-prefixed entry from log file at specific offset.

**Files to Modify**:
- `log_file.go`

**Implementation**:
```go
// Read entry at specific byte offset
func (lf *LogFile) ReadEntryAt(offset int64) (*LogEntry, error)

// Read entry at offset using streaming (for large entries)
func (lf *LogFile) ReadEntryAtStreaming(offset int64) (*LogEntry, error)
```

**Algorithm** (standard):
1. Lock for reading (RWMutex read lock)
2. Read 4 bytes at offset → get length
3. Read length bytes at offset+4 → get entry data
4. Deserialize entry data (`LogEntryFromTony`)
5. Return entry

**Algorithm** (streaming - for large entries):
1. Lock for reading (RWMutex read lock)
2. Read 4 bytes at offset → get length
3. Create `io.SectionReader` for entry data (offset+4, length)
4. Use `TokenSource` to tokenize from stream
5. Parse from tokens incrementally
6. Return entry

**Dependencies**: Step 1.1 (entry deserialization), Step 2.2 (to test), Token streaming API

**Test Cases**:
1. ✅ ReadEntryAt reads correct entry
2. ✅ ReadEntryAt at offset 0 reads first entry
3. ✅ ReadEntryAt at offset N reads Nth entry
4. ✅ ReadEntryAt returns error for invalid offset
5. ✅ ReadEntryAt returns error for truncated entry (length > available bytes)
6. ✅ ReadEntryAt returns error for corrupted entry data
7. ✅ Round-trip: AppendEntry → ReadEntryAt → compare
8. ✅ ReadEntryAtStreaming works for large entries (doesn't load into memory)

**Verification**:
- [ ] `go test -v ./storage -run TestLogFileRead`
- [ ] Can read back all written entries
- [ ] Error handling works correctly
- [ ] Round-trip produces identical entries
- [ ] Streaming version works for large entries

**Checkpoint**: ✅ Can read entries from log file at specific offsets (standard and streaming)

---

### Step 2.4: Sequential Read Iterator

**Goal**: Read entries sequentially from log file.

**Files to Modify**:
- `log_file.go`

**Implementation**:
```go
// Iterator for reading entries sequentially
type LogFileIterator struct {
    file   *LogFile
    offset int64
}

// Create iterator starting at offset
func (lf *LogFile) Iterator(offset int64) *LogFileIterator

// Read next entry, returns entry and new offset
func (it *LogFileIterator) Next() (*LogEntry, int64, error)

// Check if iterator is at end
func (it *LogFileIterator) Done() bool
```

**Dependencies**: Step 2.3 (ReadEntryAt)

**Test Cases**:
1. ✅ Iterator reads entries sequentially
2. ✅ Iterator starts at correct offset
3. ✅ Iterator.Next() increments offset correctly
4. ✅ Iterator.Done() returns true at end of file
5. ✅ Iterator handles empty file correctly
6. ✅ Iterator handles corrupted entries (skip or error?)

**Verification**:
- [ ] `go test -v ./storage -run TestLogFileIterator`
- [ ] Iterator reads all entries in order
- [ ] Offset increments correctly

**Checkpoint**: ✅ Can iterate through log file entries

---

## Phase 3: Transaction Buffer

### Step 3.1: Transaction Buffer Structure

**Goal**: Create buffer to accumulate patches during transaction.

**Files to Create**:
- `tx_buffer.go`

**Implementation**:
```go
type TransactionBuffer struct {
    patches []PatchEntry
    mu      sync.Mutex
}

type PatchEntry struct {
    Path string      // Kinded path (e.g., "a.b")
    Diff *ir.Node    // Diff for this path
}

// Create new transaction buffer
func NewTransactionBuffer() *TransactionBuffer

// Add patch to buffer
func (tb *TransactionBuffer) AddPatch(path string, diff *ir.Node)

// Get all patches
func (tb *TransactionBuffer) Patches() []PatchEntry

// Clear buffer
func (tb *TransactionBuffer) Clear()
```

**Dependencies**: None

**Test Cases**:
1. ✅ AddPatch adds patch to buffer
2. ✅ Multiple AddPatch calls accumulate patches
3. ✅ Patches() returns all patches
4. ✅ Clear() empties buffer
5. ✅ Concurrent AddPatch calls are safe

**Verification**:
- [ ] `go test -v ./storage -run TestTransactionBuffer`
- [ ] Patches accumulated correctly
- [ ] Thread-safe

**Checkpoint**: ✅ Can accumulate patches in buffer

---

### Step 3.2: Merge Patches to Root Diff

**Goal**: Merge all patches into single root diff.

**Files to Modify**:
- `tx_buffer.go`

**Implementation**:
```go
// Merge all patches into root diff
func (tb *TransactionBuffer) MergeToRoot() (*ir.Node, error)
```

**Algorithm**:
1. Start with empty root diff (`ir.Node` with ObjectType)
2. For each patch:
   - Parse kinded path (e.g., "a.b.c" → PathSegment structure)
   - Traverse/create nested structure in root diff
   - Set value at path
3. Return merged root diff

**Dependencies**: Step 1.2 (path extraction - reverse operation)

**Test Cases**:
1. ✅ MergeToRoot with single patch: `a.b` → `{a: {b: <diff>}}`
2. ✅ MergeToRoot with nested patch: `a.b.c` → `{a: {b: {c: <diff>}}}`
3. ✅ MergeToRoot with multiple patches: `a.b` and `a.c` → `{a: {b: <diff>, c: <diff>}}`
4. ✅ MergeToRoot with different roots: `a.b` and `x.y` → `{a: {b: <diff>}, x: {y: <diff>}}`
5. ✅ MergeToRoot handles empty buffer → empty diff
6. ✅ MergeToRoot preserves existing structure (if merging into existing diff)

**Verification**:
- [ ] `go test -v ./storage -run TestMergeToRoot`
- [ ] Merged diff structure is correct
- [ ] All patches included in root diff
- [ ] Can extract paths from merged diff (use Step 1.2)

**Checkpoint**: ✅ Can merge patches into root diff

---

### Step 3.3: Serialize Buffer for Append

**Goal**: Convert transaction buffer to `LogEntry` ready for append.

**Files to Modify**:
- `tx_buffer.go`

**Implementation**:
```go
// Create LogEntry from buffer (ready for append)
func (tb *TransactionBuffer) ToLogEntry(commit int64, seq int64, timestamp string) (*LogEntry, error)
```

**Algorithm**:
1. Merge patches to root diff (`MergeToRoot()`)
2. Create `LogEntry` with:
   - Commit: commit
   - Seq: seq
   - Timestamp: timestamp
   - Diff: merged root diff
3. Return `LogEntry`

**Dependencies**: Step 3.2 (MergeToRoot), Step 1.1 (LogEntry struct)

**Test Cases**:
1. ✅ ToLogEntry creates LogEntry with correct fields
2. ✅ ToLogEntry merges patches correctly
3. ✅ ToLogEntry with empty buffer → LogEntry with empty diff
4. ✅ ToLogEntry can be serialized (use Step 1.1)

**Verification**:
- [ ] `go test -v ./storage -run TestBufferToLogEntry`
- [ ] LogEntry has correct structure
- [ ] Can serialize LogEntry (round-trip)

**Checkpoint**: ✅ Can convert buffer to LogEntry

---

## Phase 4: Index Integration

### Step 4.1: Update LogSegment Structure

**Goal**: Add `LogPosition` and `KindedPath` fields to `LogSegment`.

**Files to Modify**:
- `index/log_segment.go`

**Implementation**:
```go
type LogSegment struct {
    StartCommit int64
    StartTx     int64
    EndCommit   int64
    EndTx       int64
    KindedPath  string  // Kinded path (used for querying and extraction; "" for root) - replaces RelPath
    LogPosition int64   // NEW: Byte offset in log file
}
```

**Note**: `RelPath` field is removed as it was redundant with `KindedPath` in the new design (all entries at root).

**Dependencies**: None (just adding fields)

**Test Cases**:
1. ✅ LogSegment can be created with new fields
2. ✅ Existing code still works (backward compatibility?)
3. ✅ Serialization/deserialization works (if using gob/json)

**Verification**:
- [ ] `go test -v ./index -run TestLogSegment`
- [ ] No compilation errors
- [ ] Existing tests still pass

**Checkpoint**: ✅ LogSegment has new fields

---

### Step 4.2: Index Operations with LogPosition

**Goal**: Update index to store/query LogPosition and KindedPath.

**Files to Modify**:
- `index/index.go`

**Implementation**:
```go
// Add entry to index with LogPosition and KindedPath
func (idx *Index) AddWithLogPosition(path string, logPosition int64, kindedPath string, commit int64, tx int64) error

// Query index for paths at commit, returns segments with LogPosition
func (idx *Index) QueryLogPositions(path string, level int, maxCommit int64) ([]*LogSegment, error)
```

**Dependencies**: Step 4.1 (LogSegment with new fields)

**Test Cases**:
1. ✅ AddWithLogPosition adds segment with LogPosition
2. ✅ QueryLogPositions returns segments with LogPosition
3. ✅ Multiple paths can point to same LogPosition (different KindedPath)
4. ✅ Query filters by commit correctly
5. ✅ Query filters by level correctly

**Verification**:
- [ ] `go test -v ./index -run TestIndexLogPosition`
- [ ] Can add and query entries
- [ ] LogPosition stored correctly

**Checkpoint**: ✅ Index stores/returns LogPosition and KindedPath

---

### Step 4.3: Index from Log Entry

**Goal**: Index all paths from a LogEntry (for recovery).

**Files to Create**:
- `index/recovery.go`

**Implementation**:
```go
// Index all paths from a LogEntry
// Extracts all paths from entry.Diff and creates index entries with KindedPath
func (idx *Index) IndexLogEntry(entry *LogEntry, logPosition int64, level int) error
```

**Algorithm**:
1. Extract all paths from entry.Diff (use Step 1.2: `ExtractAll` from kindedpath package)
2. For each path (already in kinded path format):
   - Create LogSegment with KindedPath (not RelPath - RelPath removed)
   - Set LogPosition from parameter
   - Set commit/tx from entry
   - Add segment to index

**Dependencies**: Step 1.2 (ExtractAll from kindedpath), Step 4.2 (AddWithLogPosition)

**Test Cases**:
1. ✅ IndexLogEntry indexes all paths from diff
2. ✅ IndexLogEntry sets correct LogPosition
3. ✅ IndexLogEntry sets correct KindedPath
4. ✅ IndexLogEntry sets correct commit/tx
5. ✅ IndexLogEntry handles empty diff correctly
6. ✅ IndexLogEntry handles nested paths correctly

**Verification**:
- [ ] `go test -v ./index -run TestIndexLogEntry`
- [ ] All paths from diff are indexed
- [ ] KindedPath is correct for each path

**Checkpoint**: ✅ Can index all paths from LogEntry

---

## Phase 5: Write Operations Integration

### Step 5.1: Write Transaction to Log

**Goal**: Write transaction buffer to log file and update index.

**Files to Create**:
- `write.go`

**Implementation**:
```go
// Write transaction buffer to log and update index
func (s *Storage) WriteTransaction(buffer *TransactionBuffer, commit int64, seq int64, level int) error
```

**Algorithm**:
1. Convert buffer to LogEntry (`buffer.ToLogEntry()`)
2. Open log file for level (`OpenLogFile()`)
3. Append entry to log (`logFile.AppendEntry()`)
4. Get LogPosition from append
5. Index all paths from entry (`index.IndexLogEntry()`)
6. Close log file

**Dependencies**: 
- Step 3.3 (ToLogEntry)
- Step 2.1 (OpenLogFile)
- Step 2.2 (AppendEntry)
- Step 4.3 (IndexLogEntry)

**Test Cases**:
1. ✅ WriteTransaction writes entry to log
2. ✅ WriteTransaction updates index
3. ✅ WriteTransaction sets correct LogPosition
4. ✅ WriteTransaction indexes all paths
5. ✅ WriteTransaction handles errors correctly

**Verification**:
- [ ] `go test -v ./storage -run TestWriteTransaction`
- [ ] Entry written to correct log file
- [ ] Index contains all paths
- [ ] Can read back entry at LogPosition

**Checkpoint**: ✅ Can write transaction to log and index

---

### Step 5.2: Integration with Existing Transaction System

**Goal**: Integrate new write path with existing `tx.go` commit flow.

**Files to Modify**:
- `tx.go`

**Implementation**:
- Modify `Commit()` method to:
  1. Create `TransactionBuffer` from transaction state
  2. Call `WriteTransaction()` instead of old write path
  3. Update index persistence

**Dependencies**: Step 5.1 (WriteTransaction)

**Test Cases**:
1. ✅ Commit writes to new log format
2. ✅ Commit updates index correctly
3. ✅ Commit handles errors correctly
4. ✅ Existing transaction tests still pass

**Verification**:
- [ ] `go test -v ./storage -run TestTxCommit`
- [ ] Integration tests pass
- [ ] Old tests updated or removed

**Checkpoint**: ✅ Transaction commit uses new write path

---

## Phase 6: Read Operations Integration

### Step 6.1: Read Path at Commit

**Goal**: Read state at kinded path for given commit.

**Files to Create**:
- `read.go`

**Implementation**:
```go
// Read state at kinded path for given commit
func (s *Storage) ReadAt(kindedPath string, commit int64) (*ir.Node, error)
```

**Detailed Algorithm**: See `read_algorithm.md` for complete specification.

**High-Level Steps**:
1. Get index iterator at `kindedPath` (`index.IterAtPath()`)
2. Determine starting level using compaction alignment
3. Read compacted segment (if `startingLevel > 0`) and use as base state
4. Iterate commits starting from commit in descending order using iterator (`it.CommitsAt(commit, Down)`):
   - `CommitsAt` seeks to commit and iterates downward (segments <= commit)
   - Skip segments covered by compacted segment
   - Read entry at `LogPosition`
   - Extract diff using `KindedPath`
   - Apply diff to result
   - Stop early when all necessary segments are read
5. Extract final path if needed
6. Return result

**Key Optimization**: Uses index iterators to iterate commits efficiently without materializing all segments. Stops early when all necessary history is read.

**Dependencies**:
- Step 4.2 (QueryLogPositions)
- Step 2.3 (ReadEntryAt)
- Step 1.2 (kindedpath.Get)
- Step 1.4 (applyDiff)

**Test Cases**:
1. ✅ ReadAt reads correct state at root (`""`)
2. ✅ ReadAt reads correct state at nested path (`"a.b.c"`)
3. ✅ ReadAt handles multiple entries (applies in commit order)
4. ✅ ReadAt handles compacted segments (uses as base, filters covered)
5. ✅ ReadAt extracts paths correctly (from root diffs)
6. ✅ ReadAt handles non-existent paths (returns empty state)
7. ✅ ReadAt handles empty state (no segments)
8. ✅ ReadAt handles ancestor segments (extracts correctly)
9. ✅ ReadAt handles corrupted entries (returns error)
10. ✅ ReadAt handles compaction boundaries (uses compacted segment)

**Verification**:
- [ ] `go test -v ./storage -run TestReadAt`
- [ ] All test cases pass
- [ ] Reads match expected state
- [ ] Algorithm matches specification in `read_algorithm.md`

**Checkpoint**: ✅ Can read state at kinded path for commit

---

### Step 6.2: Integration with Existing Read System

**Goal**: Replace old `ReadAt` with new implementation.

**Files to Modify**:
- `storage.go` (or wherever ReadAt is defined)

**Implementation**:
- Replace old `ReadAt` implementation with new one
- Update callers if needed

**Dependencies**: Step 6.1 (ReadAt)

**Test Cases**:
1. ✅ New ReadAt works with existing callers
2. ✅ Existing read tests pass
3. ✅ Performance is acceptable

**Verification**:
- [ ] `go test -v ./storage -run TestRead`
- [ ] All existing tests pass

**Checkpoint**: ✅ Read operations use new implementation

---

## Phase 7: Recovery & Index Rebuild

### Step 7.1: Rebuild Index from Logs

**Goal**: Rebuild index by scanning log files.

**Files to Create**:
- `recovery.go`

**Implementation**:
```go
// Rebuild index from logs starting at maxCommit
func (s *Storage) RebuildIndex(maxCommit int64) error
```

**Algorithm**:
1. For each level:
   - Open log file
   - Create iterator starting at offset 0
   - For each entry:
     - If entry.Commit > maxCommit:
       - Index entry (`index.IndexLogEntry()`)
     - Move to next entry
   - Close log file
2. Persist index with new MaxCommit

**Dependencies**:
- Step 2.4 (Iterator)
- Step 4.3 (IndexLogEntry)
- Step 4.2 (Index operations)

**Test Cases**:
1. ✅ RebuildIndex scans all log files
2. ✅ RebuildIndex indexes entries after maxCommit
3. ✅ RebuildIndex skips entries before maxCommit
4. ✅ RebuildIndex handles corrupted entries (skip or error?)
5. ✅ RebuildIndex updates MaxCommit

**Verification**:
- [ ] `go test -v ./storage -run TestRebuildIndex`
- [ ] Index rebuilt correctly
- [ ] Can query rebuilt index

**Checkpoint**: ✅ Can rebuild index from logs

---

### Step 7.2: Startup Recovery

**Goal**: Load persisted index or rebuild from logs on startup.

**Files to Modify**:
- `storage.go` (Open/NewStorage)

**Implementation**:
```go
// Open storage with recovery
func OpenStorage(baseDir string) (*Storage, error)
```

**Algorithm**:
1. Try to load persisted index (`LoadIndexWithMetadata()`)
2. If successful:
   - Load index and MaxCommit
   - Rebuild incrementally from MaxCommit+1 (`RebuildIndex()`)
3. If failed (corrupted/missing):
   - Rebuild from beginning (`RebuildIndex(0)`)
4. Return Storage

**Dependencies**: Step 7.1 (RebuildIndex), existing `LoadIndexWithMetadata`

**Test Cases**:
1. ✅ OpenStorage loads persisted index
2. ✅ OpenStorage rebuilds incrementally
3. ✅ OpenStorage rebuilds from scratch if index missing
4. ✅ OpenStorage handles corrupted index

**Verification**:
- [ ] `go test -v ./storage -run TestOpenStorage`
- [ ] Startup works correctly
- [ ] Index is correct after startup

**Checkpoint**: ✅ Storage can recover on startup

---

## Phase 8: Token Streaming Integration

### Step 8.1: Streaming Read for Large Entries

**Goal**: Use `TokenSource` for reading large entries/sub-documents without loading into memory.

**Files to Create/Modify**:
- `log_file.go` (add streaming read methods)
- `read_streaming.go` (new file for streaming read operations)

**Implementation**:
```go
// Read sub-document from offset using streaming
func (lf *LogFile) ReadSubDocumentAt(offset int64, size int64) (*ir.Node, error) {
    sectionReader := io.NewSectionReader(lf.file, offset, size)
    source := token.NewTokenSource(sectionReader)
    
    // Parse directly from TokenSource (streaming, no token collection)
    return parse.ParseFromTokenSource(source)
}
```

**Dependencies**: Token streaming API (`TokenSource`), Step 2.3 (ReadEntryAt)

**Test Cases**:
1. ✅ ReadSubDocumentAt reads correct sub-document
2. ✅ ReadSubDocumentAt doesn't load entire entry into memory
3. ✅ ReadSubDocumentAt works with `io.SectionReader`
4. ✅ ReadSubDocumentAt handles large entries correctly
5. ✅ ReadSubDocumentAt produces same result as non-streaming read

**Verification**:
- [ ] `go test -v ./storage -run TestStreamingRead`
- [ ] Memory usage is bounded (check with profiling)
- [ ] Works with entries larger than available memory

**Checkpoint**: ✅ Can read large entries/sub-documents using streaming

---

### Step 8.2: Streaming Write with Offset Tracking

**Goal**: Use `TokenSink` for writing entries with offset tracking for indexing.

**Files to Create/Modify**:
- `log_file.go` (add streaming write methods)
- `write_streaming.go` (new file for streaming write operations)

**Implementation**:
```go
// Write entry with offset tracking (for snapshots with indexing)
func (lf *LogFile) AppendEntryWithOffsets(entry *LogEntry) (int64, map[string]int64, error) {
    // Write length prefix (placeholder)
    lengthPos := lf.Position()
    lengthBytes := make([]byte, 4)
    lf.Write(lengthBytes)
    
    // Create TokenSink with offset tracking
    pathOffsets := make(map[string]int64)
    sink := token.NewTokenSink(lf.file, func(offset int, path string, tok token.Token) {
        // Record path offsets for indexing
        pathOffsets[path] = int64(offset)
    })
    
    // Tokenize and write entry
    tokens := tokenizeEntry(entry)
    if err := sink.Write(tokens); err != nil {
        return 0, nil, err
    }
    
    // Update length prefix
    entrySize := sink.Offset()
    binary.BigEndian.PutUint32(lengthBytes, uint32(entrySize))
    lf.WriteAt(lengthBytes, lengthPos)
    
    logPosition := lengthPos
    return logPosition, pathOffsets, nil
}
```

**Dependencies**: Token streaming API (`TokenSink`), Step 2.2 (AppendEntry)

**Test Cases**:
1. ✅ AppendEntryWithOffsets writes entry correctly
2. ✅ AppendEntryWithOffsets tracks offsets correctly
3. ✅ AppendEntryWithOffsets returns correct path offsets
4. ✅ AppendEntryWithOffsets produces same output as standard AppendEntry
5. ✅ Offset tracking works for nested paths

**Verification**:
- [ ] `go test -v ./storage -run TestStreamingWrite`
- [ ] Offsets are tracked correctly
- [ ] Can read back entries written with offset tracking

**Checkpoint**: ✅ Can write entries with offset tracking for indexing

---

### Step 8.3: Inverted Index Building During Write

**Goal**: Build inverted index using `TokenSink` callbacks during snapshot writing.

**Files to Create**:
- `inverted_index.go` (new file for inverted index operations)

**Implementation**:
```go
type InvertedIndex struct {
    PathToSubDocs map[string][]SubDocRef
    SubDocs       map[SubDocID]SubDocMeta
}

type SubDocRef struct {
    SubDocID SubDocID
    Offset   int64
    Size     int64
}

// Build inverted index during snapshot write
func buildInvertedIndexDuringWrite(writer io.Writer, nodes map[string]*ir.Node) (*InvertedIndex, error) {
    index := &InvertedIndex{
        PathToSubDocs: make(map[string][]SubDocRef),
        SubDocs: make(map[SubDocID]SubDocMeta),
    }
    
    subDocID := SubDocID(0)
    sink := token.NewTokenSink(writer, func(offset int, path string, tok token.Token) {
        // Record sub-document start
        ref := SubDocRef{
            SubDocID: subDocID,
            Offset:   int64(offset),
            // Size will be calculated when sub-doc ends
        }
        index.PathToSubDocs[path] = append(index.PathToSubDocs[path], ref)
    })
    
    // Write nodes with offset tracking
    for path, node := range nodes {
        tokens := tokenizeNode(node)
        if err := sink.Write(tokens); err != nil {
            return nil, err
        }
        subDocID++
    }
    
    return index, nil
}
```

**Dependencies**: Step 8.2 (Streaming write with offset tracking)

**Test Cases**:
1. ✅ BuildInvertedIndex tracks sub-document offsets
2. ✅ BuildInvertedIndex maps paths to sub-documents correctly
3. ✅ BuildInvertedIndex handles nested paths correctly
4. ✅ BuildInvertedIndex can be used to read sub-documents later

**Verification**:
- [ ] `go test -v ./storage -run TestInvertedIndex`
- [ ] Index structure is correct
- [ ] Can use index to read sub-documents

**Checkpoint**: ✅ Can build inverted index during snapshot write

---

## Phase 9: Testing & Validation

### Step 8.1: Integration Tests

**Goal**: End-to-end tests of write/read operations.

**Files to Create**:
- `storage_integration_test.go`

**Test Cases**:
1. ✅ Write transaction → read back
2. ✅ Write multiple transactions → read at different commits
3. ✅ Write nested paths → read parent and child
4. ✅ Write → restart → read (recovery)
5. ✅ Write → compact → read (if compaction implemented)
6. ✅ Concurrent writes (if supported)
7. ✅ Large transactions (stress test)

**Verification**:
- [ ] `go test -v ./storage -run TestIntegration`
- [ ] All integration tests pass
- [ ] Performance is acceptable

**Checkpoint**: ✅ End-to-end functionality works

---

### Step 8.2: Performance Testing

**Goal**: Validate performance characteristics.

**Files to Create**:
- `storage_bench_test.go`

**Benchmarks**:
1. ✅ BenchmarkAppendEntry
2. ✅ BenchmarkReadEntryAt
3. ✅ BenchmarkReadAt
4. ✅ BenchmarkRebuildIndex
5. ✅ BenchmarkWriteTransaction

**Verification**:
- [ ] `go test -bench=. ./storage`
- [ ] Performance meets requirements
- [ ] No regressions

**Checkpoint**: ✅ Performance is acceptable

---

### Step 8.3: Error Handling & Edge Cases

**Goal**: Comprehensive error handling.

**Test Cases**:
1. ✅ Corrupted log entries
2. ✅ Truncated log files
3. ✅ Missing log files
4. ✅ Invalid ExtractPath
5. ✅ Empty diffs
6. ✅ Very large entries
7. ✅ Disk full scenarios

**Verification**:
- [ ] `go test -v ./storage -run TestErrorHandling`
- [ ] All error cases handled gracefully
- [ ] No panics

**Checkpoint**: ✅ Robust error handling

---

## Phase 10: Cleanup & Documentation

### Step 9.1: Remove Old Code

**Goal**: Remove obsolete code from old storage system.

**Files to Delete/Modify**:
- Old diff file code (if not needed)
- Old read/write paths
- Unused functions

**Verification**:
- [ ] `go build ./storage` succeeds
- [ ] All tests pass
- [ ] No dead code

**Checkpoint**: ✅ Old code removed

---

### Step 9.2: Update Documentation

**Goal**: Update code comments and documentation.

**Files to Update**:
- All new files (add godoc comments)
- `README.md` (if exists)
- Design documents (mark as implemented)

**Verification**:
- [ ] `go doc ./storage` shows documentation
- [ ] Design docs updated

**Checkpoint**: ✅ Documentation complete

---

## Verification Checklist

After each phase, verify:

- [ ] All tests pass: `go test ./storage ./index`
- [ ] No compilation errors: `go build ./storage`
- [ ] Code follows project style
- [ ] Error handling is comprehensive
- [ ] Performance is acceptable

## Dependencies Graph

```
Phase 1 (Foundation)
├── Step 1.1: Entry Serialization
├── Step 1.2: Path Extraction
└── Step 1.3: Path Conversion

Phase 2 (I/O)
├── Step 2.1: LogFile Structure
├── Step 2.2: Append Entry (depends on 1.1)
├── Step 2.3: Read Entry (depends on 1.1, 2.2)
└── Step 2.4: Iterator (depends on 2.3)

Phase 3 (Buffer)
├── Step 3.1: Buffer Structure
├── Step 3.2: Merge to Root (depends on 1.2)
└── Step 3.3: ToLogEntry (depends on 3.2, 1.1)

Phase 4 (Index)
├── Step 4.1: Update LogSegment
├── Step 4.2: Index Operations (depends on 4.1)
└── Step 4.3: Index from Entry (depends on 1.2, 1.3, 4.2)

Phase 5 (Write Integration)
├── Step 5.1: Write Transaction (depends on 3.3, 2.1, 2.2, 4.3)
└── Step 5.2: Tx Integration (depends on 5.1)

Phase 6 (Read Integration)
├── Step 6.1: ReadAt (depends on 4.2, 2.3, 1.2)
└── Step 6.2: Read Integration (depends on 6.1)

Phase 7 (Recovery)
├── Step 7.1: Rebuild Index (depends on 2.4, 4.3)
└── Step 7.2: Startup Recovery (depends on 7.1)

Phase 8 (Token Streaming)
├── Step 8.1: Streaming Read (depends on 2.3)
├── Step 8.2: Streaming Write (depends on 2.2)
└── Step 8.3: Inverted Index (depends on 8.2)

Phase 9 (Testing)
└── All previous steps

Phase 10 (Cleanup)
└── All previous steps
```

## Estimated Timeline

- **Phase 1**: 2-3 days (foundation)
- **Phase 2**: 2-3 days (I/O)
- **Phase 3**: 1-2 days (buffer)
- **Phase 4**: 2-3 days (index)
- **Phase 5**: 1-2 days (write integration)
- **Phase 6**: 2-3 days (read integration)
- **Phase 7**: 2-3 days (recovery)
- **Phase 8**: 2-3 days (testing)
- **Phase 9**: 1 day (cleanup)

**Total**: ~18-28 days (includes token streaming integration)

## Success Criteria

✅ **Complete when**:
1. All phases implemented and tested
2. All tests pass
3. Performance meets requirements
4. Error handling is comprehensive
5. Documentation is complete
6. Old code removed
7. Integration tests pass

## Notes

- Each step should be implemented and tested before moving to next
- Use TDD (Test-Driven Development) where possible
- Keep commits small and focused
- Review code after each phase
- Update this plan if design changes during implementation
