# Read Path Sanity Check: Bottom-Up Alignment

## Goal

Verify that the bottom-level components we're building actually support the higher-level read operations we need.

## Higher-Level Read Requirements

From `redesign_outline.md`:

1. **ReadAt(path, commit N)**: Read state at path `/a` at commit N
2. **Query index**: Get all segments for `/a` and `/a/*` at commit N
3. **Determine starting level**: Use compaction alignment (N % divisor^L == 0)
4. **Read base state**: If starting level > 0, read compacted segment
5. **Filter segments**: Remove segments covered by compacted segment
6. **Sort by LogPosition**: Sort byte offsets for sequential reads
7. **Read sequentially**: Read from log file at sorted offsets, apply diffs in commit order

## Bottom-Level Components We're Building

1. **Log File I/O**: `io.ReaderAt` for reading at byte offsets
2. **Entry Serialization**: `LogEntry.ToTony()` / `FromTony()`
3. **Path Operations**: Parse kinded path string → `PathSegment` (for operations)
4. **Index Integration**: Query index for log positions

## Sanity Check: Trace Through Concrete Read Example

### Example: Read `/a` at commit 5

**Assumptions**:
- Divisor = 2
- Level 0: commits 0-5 (no alignment)
- Level 1: commit 4 (4 % 2 == 0, alignment point)
- Entries:
  - Level 0: `/a/b` at commit 1, `/a` at commit 2, `/a/c` at commit 3
  - Level 1: `/a` at commit 4 (compacted segment covering commits 0-4)

### Step-by-Step Trace

#### Step 1: Query Index for `/a` at commit 5

**What we need**:
- Query index: "Give me all segments for `/a` at commit 5"
- Index returns: `[]LogSegment` with `LogPosition` (byte offset), `StartCommit`, `EndCommit`

**Can we do this?** ✅ **YES**
- Index has `QueryLogPositions(path, level, commit)` → returns `[]int64` (log positions)
- We can map log positions to `LogSegment` entries

**Verification**:
```go
// Index query
segments := index.Query(path="/a", level=0, maxCommit=5)
// Returns: [
//   {LogPosition: 100, StartCommit: 1, EndCommit: 1, Path: "/a/b"},
//   {LogPosition: 200, StartCommit: 2, EndCommit: 2, Path: "/a"},
//   {LogPosition: 300, StartCommit: 3, EndCommit: 3, Path: "/a/c"}
// ]
```

#### Step 2: Determine Starting Level (Compaction Alignment)

**What we need**:
- Check: Is commit 5 aligned at level 1? (5 % 2 == 1, NO)
- Check: Is commit 5 aligned at level 0? (Always YES)
- **Result**: Start at level 0 (no compacted base)

**Can we do this?** ✅ **YES**
- Simple math: `commit % (divisor^level) == 0`
- No bottom-level component needed (just math)

**Verification**:
```go
// Alignment check
level := 0  // Always correct at level 0
// No compacted segment to read
```

#### Step 3: Filter Segments by Commit

**What we need**:
- Filter segments: `StartCommit <= 5`
- Keep: segments with commits <= 5

**Can we do this?** ✅ **YES**
- Simple filtering on `LogSegment.StartCommit`
- No bottom-level component needed

**Verification**:
```go
// Filter segments
filtered := []LogSegment{}
for _, seg := range segments {
    if seg.StartCommit <= 5 {
        filtered = append(filtered, seg)
    }
}
// Result: All 3 segments (commits 1, 2, 3)
```

#### Step 4: Sort Segments by LogPosition (Byte Offset)

**What we need**:
- Sort segments by `LogPosition` (byte offset)
- Result: Sequential reads (100, 200, 300)

**Can we do this?** ✅ **YES**
- Simple sort on `LogSegment.LogPosition`
- No bottom-level component needed

**Verification**:
```go
// Sort by LogPosition
sort.Slice(filtered, func(i, j int) bool {
    return filtered[i].LogPosition < filtered[j].LogPosition
})
// Result: [100, 200, 300] (already sorted)
```

#### Step 5: Read Entries from Log File at Byte Offsets

**What we need**:
- Open `level0.log` file
- Read entry at byte offset 100
- Read entry at byte offset 200
- Read entry at byte offset 300

**Can we do this?** ✅ **YES** (with `io.ReaderAt`)
```go
// Open log file
logFile, err := os.Open("level0.log")
defer logFile.Close()

// Read at byte offset 100
reader := io.NewSectionReader(logFile, 100, math.MaxInt64)
entry1, err := LogEntryFromTony(reader)  // Parse from reader

// Read at byte offset 200
reader = io.NewSectionReader(logFile, 200, math.MaxInt64)
entry2, err := LogEntryFromTony(reader)

// Read at byte offset 300
reader = io.NewSectionReader(logFile, 300, math.MaxInt64)
entry3, err := LogEntryFromTony(reader)
```

**Question**: How does `LogEntryFromTony()` know where entry ends?
- **Answer**: Tony wire format parser consumes entire entry, returns bytes consumed
- **Need**: `LogEntryFromTony(reader io.Reader) (*LogEntry, int64, error)` → returns entry, bytes read, error

#### Step 6: Parse Path and Extract Subtree (CRITICAL - User's Insight)

**User's Insight**: When reading `/a/b`, we read the entry at `/a` and extract the `b` part.

**What we need**:
- Entry has `Path: "/a"` and `Diff: {b: {y: 2}}`
- Index entry has `ExtractKey: "b"` (if reading child)
- Need to extract `b` from parent diff

**Can we do this?** ✅ **YES** (with ExtractKey field)
```go
// Read /a/b at commit 2
segments := index.Query("/a/b", commit=2)
// Returns: [{LogPosition: 200, ExtractKey: "b"}]

entry := ReadEntryAt(logFile, 200)
// Entry.Path = "/a", Entry.Diff = {b: {y: 2}}

// Extract b part from parent diff
if segment.ExtractKey != "" {
    childDiff := entry.Diff.GetField(segment.ExtractKey)  // Extract "b"
    // childDiff = {y: 2}
    return childDiff
} else {
    return entry.Diff  // Reading parent, return entire diff
}
```

**Key Change**: Index points `/a/b` to entry at `/a` with `ExtractKey: "b"`. We extract the child from the parent diff.

#### Step 7: Apply Diffs in Commit Order

**What we need**:
- Sort entries by commit (already have from index)
- Apply diffs sequentially to build state

**Can we do this?** ✅ **YES**
- Simple diff application (Tony format supports this)
- No bottom-level component needed

**Verification**:
```go
// Apply diffs in commit order
state := ir.EmptyNode()  // Start with empty state
for _, entry := range sortedEntries {
    state = applyDiff(state, entry.Diff)
}
```

## Revised Schema: Path as String

Based on user's remark, let's reconsider:

**Option A: Path as `*PathSegment` (current)**
- Pros: Type-safe, structured
- Cons: More complex serialization, larger on disk

**Option B: Path as `string` (user's suggestion)**
- Pros: Simpler serialization, more compact, easier to read
- Cons: Need to parse for operations

**Recommendation**: **Option B** - Store as string, parse when needed

```go
type LogEntry struct {
    Commit    int64      // Commit number
    Seq       int64      // Transaction sequence
    Path      string     // Kinded path string (e.g., "a.b[0]")
    Timestamp string     // Timestamp
    Diff      *ir.Node   // The actual diff
}

// Parse path when needed for operations
func (e *LogEntry) PathSegment() (*PathSegment, error) {
    return ParseKindedPath(e.Path)
}
```

## Bottom-Level Components Needed (Revised)

### Level 0: Absolute Prerequisites

1. ✅ **Log File I/O**: Use `io.ReaderAt` + `io.NewSectionReader`
   ```go
   func ReadEntryAt(file *os.File, offset int64) (*LogEntry, int64, error)
   // Returns: entry, bytes read, error
   ```

2. ✅ **Entry Serialization**: `LogEntry.ToTony()` / `FromTony()`
   ```go
   func (e *LogEntry) ToTony() ([]byte, error)
   func LogEntryFromTony(reader io.Reader) (*LogEntry, int64, error)
   // Returns: entry, bytes consumed, error
   ```

3. ✅ **Path Parsing**: Parse kinded path string → `PathSegment`
   ```go
   func ParseKindedPath(pathStr string) (*PathSegment, error)
   func (ps *PathSegment) String() string
   func (ps *PathSegment) IsChildOf(parent *PathSegment) bool
   ```

### Level 1: Core Operations

4. ✅ **Log File Append**: Append entry, return byte offset
   ```go
   func AppendEntry(file *os.File, entry *LogEntry) (int64, error)
   // Returns: LogPosition (byte offset), error
   ```

5. ✅ **Index Integration**: Add/query log positions
   ```go
   func (idx *Index) AddWithLogPosition(seg *LogSegment, logPosition int64)
   func (idx *Index) QueryLogPositions(path string, level int, commit int64) []LogSegment
   ```

## Sanity Check Results (Updated with User's Insight)

### ✅ Can We Do Everything?

1. **Query index**: ✅ Yes - index has log positions + ExtractKey
2. **Read at byte offset**: ✅ Yes - `io.ReaderAt` + `io.NewSectionReader`
3. **Parse entries**: ✅ Yes - `LogEntryFromTony()` with reader
4. **Extract subtree**: ✅ Yes - `entry.Diff.GetField(ExtractKey)` if ExtractKey != ""
5. **Sort by offset**: ✅ Yes - simple sort on `LogPosition`
6. **Apply diffs**: ✅ Yes - standard diff application (or extract child first)

### ⚠️ Critical Change Required

**Index Structure**: Need to add `LogPosition` and `ExtractKey` fields to `LogSegment`:
- `LogPosition`: Byte offset in log file
- `ExtractKey`: Key to extract from parent diff ("" if reading parent, "b" if reading `/a/b`)

**Write Logic**: When writing `/a/b`, write diff at `/a` with `{b: <diff>}`, index both paths to same entry.

### ⚠️ Open Questions

1. **Tony wire format parsing**: How does parser know entry boundaries?
   - **Answer**: Parser consumes entire entry, returns bytes consumed
   - **Need**: `LogEntryFromTony(reader io.Reader) (*LogEntry, int64, error)`

2. **Path storage**: String vs `*PathSegment`?
   - **Answer**: **String** (user's suggestion) - simpler, parse when needed

3. **File locking**: How to handle concurrent reads/writes?
   - **Answer**: Use `sync.RWMutex` for log file access
   - **Need**: `type LogFile struct { file *os.File; mu sync.RWMutex }`

## Recommendation

**Proceed with bottom-up implementation** - the sanity check shows we can do everything we need:

1. ✅ Start with path parsing (`ParseKindedPath()`)
2. ✅ Then entry serialization (`LogEntry.ToTony()` / `FromTony()`)
3. ✅ Then log file I/O (`ReadEntryAt()` using `io.ReaderAt`)
4. ✅ Then index integration
5. ✅ Then read operations

**Schema change**: Store `Path` as `string` in log, parse to `PathSegment` when needed for operations.
