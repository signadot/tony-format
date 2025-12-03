# Bottom-Up Critique: What's Missing at the Bottom?

## What We Have Defined (Top-Down)

✅ **Schema**: `LogEntry` and `PathSegment` structs with Tony schema annotations
✅ **Storage Layout**: Logs by level (`level0.log`, `level1.log`, etc.)
✅ **Index Structure**: Maps `(path, level)` → `(logPosition, commitRange)`
✅ **Write Flow**: In-memory buffer → atomic append on commit
✅ **Read Flow**: Query index → get log positions → read sequentially

## What's Missing at the Bottom Level

### 1. Log File I/O Operations (CRITICAL - Missing)

**Problem**: We've defined the schema but not the actual file operations.

**Missing**:
- ❌ How to open a log file for a given level
- ❌ How to append entries to a log file
- ❌ How to get current file size (for `LogPosition`)
- ❌ How to read an entry at a specific byte offset
- ❌ How to seek to a byte offset
- ❌ How to handle file handles (open/close, locking)
- ❌ How to handle concurrent access (file locks)

**Needed**:
```go
// Log file operations
type LogFile struct {
    level int
    path  string
    file  *os.File
    mu    sync.Mutex  // For concurrent access
}

func OpenLogFile(level int) (*LogFile, error)
func (lf *LogFile) AppendEntry(entry *LogEntry) (int64, error)  // Returns LogPosition
// Implementation:
//   1. Serialize entry to Tony format
//   2. Get current offset (file size)
//   3. Write 4-byte length prefix
//   4. Write entry data
//   5. Return offset (LogPosition)

func (lf *LogFile) ReadEntryAt(offset int64) (*LogEntry, error)
// Implementation:
//   1. Read 4 bytes at offset → get length
//   2. Read length bytes at offset+4 → get entry data
//   3. Parse entry data from Tony wire format
func (lf *LogFile) Close() error
```

**Questions**:
- How do we handle file locking? (single writer, multiple readers?)
- How do we ensure atomic appends? (O_APPEND flag? File lock?)
- How do we handle partial writes? (What if append fails mid-entry?)

### 2. Entry Serialization/Deserialization (CRITICAL - Missing)

**Problem**: We know entries are Tony wire format, but how do we actually serialize/deserialize?

**Missing**:
- ❌ How to serialize `LogEntry` to Tony wire format bytes
- ❌ How to deserialize Tony wire format bytes to `LogEntry` at a byte offset
- ❌ How to read length prefix (4 bytes) before entry data
- ❌ How to handle parsing errors (corrupt entries, partial writes)

**Needed**:
```go
// Serialization
func (e *LogEntry) ToTony() ([]byte, error)
func (e *LogEntry) FromTony(data []byte) error

// Reading from byte offset (length-prefixed)
func ReadEntryAt(file *os.File, offset int64) (*LogEntry, int64, error)
// Returns: entry, nextOffset (where next entry starts), error
// Implementation:
//   1. Read 4 bytes at offset → get length
//   2. Read length bytes at offset+4 → get entry data
//   3. Parse entry data from Tony wire format
//   4. Return entry, offset+4+length
```

**Format**: `[4 bytes: uint32 length (big-endian)][entry data in Tony wire format]`

**Questions**:
- How do we handle partial writes? (Read length but entry data is truncated - detect via ReadAt error)
- How do we handle corrupted entries? (Parser fails - return error, skip entry in recovery)

### 3. Path Operations (IMPORTANT - Missing)

**Problem**: Path is stored as string in log, but we need structured operations.

**Design Decision**: Store path as **string** in log (simpler serialization, more compact), parse to `PathSegment` when needed for operations.

**Missing**:
- ❌ How to parse kinded path string → `PathSegment`
- ❌ How to serialize `PathSegment` → string
- ❌ How to compare paths (for indexing, sorting)
- ❌ How to extract parent path from a path
- ❌ How to check if one path is a child of another

**Needed**:
```go
// Path parsing (string → PathSegment)
func ParseKindedPath(pathStr string) (*PathSegment, error)
func (ps *PathSegment) String() string  // PathSegment → string

// Path operations
func (ps *PathSegment) Parent() *PathSegment
func (ps *PathSegment) IsChildOf(parent *PathSegment) bool
func (ps *PathSegment) Compare(other *PathSegment) int  // For sorting

// Convenience method on LogEntry
func (e *LogEntry) PathSegment() (*PathSegment, error) {
    return ParseKindedPath(e.Path)
}
```

**Questions**:
- How do we parse kinded path syntax? (`a.b`, `a[0]`, `a{0}`)
- How do we handle quoted keys? (`a.'key with spaces'`)
- How do we validate path syntax?

### 4. Log File Management (IMPORTANT - Missing)

**Problem**: We know log files exist but not how to manage them.

**Missing**:
- ❌ How to determine log file path for a level (`level0.log`, `level1.log`)
- ❌ How to create log files (if they don't exist)
- ❌ How to handle log file growth (do we rotate? truncate? keep forever?)
- ❌ How to list existing log files on startup
- ❌ How to handle multiple log files per level (if we rotate)

**Needed**:
```go
// Log file management
func LogFilePath(level int) string
func EnsureLogFile(level int) error
func ListLogFiles() ([]int, error)  // Returns list of levels that have log files
```

**Questions**:
- Where do log files live? (Same directory? Subdirectory?)
- Do we create log files lazily or eagerly?
- How do we handle log file rotation? (Do we even need it?)

### 5. Byte Offset Reading (RESOLVED - Use io.ReaderAt)

**Problem**: We reference `LogPosition` (byte offset) everywhere but how do we read at offsets?

**Solution**: ✅ **Use `io.ReaderAt`** - standard Go interface for reading at specific offsets

**Implementation**:
```go
// Read entry at byte offset using io.ReaderAt
func ReadEntryAt(reader io.ReaderAt, offset int64) (*LogEntry, int64, error) {
    // Create section reader starting at offset
    sectionReader := io.NewSectionReader(reader, offset, math.MaxInt64)
    
    // Parse entry from reader (Tony parser knows boundaries)
    entry, bytesRead, err := LogEntryFromTony(sectionReader)
    if err != nil {
        return nil, 0, err
    }
    
    return entry, bytesRead, nil
}

// For appends, get current file size
func (lf *LogFile) CurrentOffset() (int64, error) {
    stat, err := lf.file.Stat()
    if err != nil {
        return 0, err
    }
    return stat.Size(), nil
}
```

**No need for explicit seek** - `io.ReaderAt` handles it!

### 6. Transaction Buffer Operations (IMPORTANT - Missing)

**Problem**: We know entries go in a buffer, but how do we manage it?

**Missing**:
- ❌ How to accumulate entries in buffer
- ❌ How to serialize buffer to bytes
- ❌ How to append buffer to log file atomically
- ❌ How to handle buffer size limits (transactions must fit in memory)

**Needed**:
```go
// Transaction buffer
type TransactionBuffer struct {
    entries []*LogEntry
    buffer  *bytes.Buffer
}

func (tb *TransactionBuffer) AddEntry(entry *LogEntry) error
func (tb *TransactionBuffer) Serialize() ([]byte, error)
func (tb *TransactionBuffer) AppendToLog(logFile *LogFile, commit int64) ([]int64, error)
// Returns: log positions for each entry
```

**Questions**:
- How do we validate entries before adding to buffer?
- How do we handle buffer size limits? (Error if too large?)
- How do we ensure atomic append? (Single write operation?)

### 7. Index Integration with Log Positions (IMPORTANT - Missing)

**Problem**: We know index stores `LogPosition`, but how do we actually use it?

**Missing**:
- ❌ How to add entries to index with log positions
- ❌ How to query index for log positions
- ❌ How to update `LogSegment` with `LogPosition` field
- ❌ How to handle index updates atomically with log writes

**Needed**:
```go
// Index operations with log positions
func (idx *Index) AddWithLogPosition(seg *LogSegment, logPosition int64)
func (idx *Index) QueryLogPositions(path string, level int, commit int64) []int64
```

**Questions**:
- How do we ensure index updates are atomic with log writes?
- How do we handle index updates if log write fails?

## Priority Order (Bottom-Up)

### Level 0: Absolute Prerequisites (Must Have)

1. **Entry Serialization/Deserialization**
   - Can't do anything without this
   - Need to convert `LogEntry` ↔ bytes
   - **Key**: `LogEntryFromTony(reader io.Reader) (*LogEntry, int64, error)` - returns bytes consumed

2. **Log File I/O Operations**
   - Can't write or read without this
   - **Reading**: Use `io.ReaderAt` + `io.NewSectionReader` (no explicit seek needed!)
   - **Writing**: Append to file, get current size for `LogPosition`

3. **Path Operations**
   - Need to parse paths for index (string → `PathSegment`)
   - Need to compare paths for queries
   - **Note**: Path stored as string in log, parse when needed

### Level 1: Core Operations (Critical)

4. **Byte Offset Tracking**
   - Need for `LogPosition`
   - Need for reading at specific offsets

5. **Transaction Buffer Operations**
   - Need for write flow
   - Need for atomic appends

6. **Index Integration**
   - Need to connect index with log positions
   - Need for read flow

### Level 2: Management (Important)

7. **Log File Management**
   - Need for startup/recovery
   - Need for file lifecycle

## What We Should Build First

**Start with Level 0** (absolute prerequisites):

1. **Path parsing**: `ParseKindedPath()` and `PathSegment.String()`
   - Can test independently
   - Needed for everything else

2. **Entry serialization**: `LogEntry.ToTony()` and `LogEntry.FromTony()`
   - Can test independently
   - Needed for file I/O

3. **Basic log file I/O**: `AppendEntry()` and `ReadEntryAt()`
   - Can test with serialization
   - Needed for everything else

**Then Level 1** (core operations):

4. **Transaction buffer**: Accumulate entries, serialize, append
5. **Index integration**: Add log positions to index
6. **Read operations**: Query index, read entries at offsets

## Open Questions

1. **Tony wire format parsing**: How does parser know entry boundaries?
   - Does `FromTony()` consume entire buffer?
   - How do we read from a specific byte offset?

2. **File locking**: How do we handle concurrent access?
   - Single writer, multiple readers?
   - How do we ensure atomic appends?

3. **Partial writes**: How do we detect/handle corrupt entries?
   - What if append fails mid-entry?
   - How do we recover?

4. **Log file location**: Where do log files live?
   - Same directory as storage root?
   - Subdirectory (`logs/level0.log`)?

5. **Index persistence**: How do we track max commit during writes?
   - Update max commit as we write?
   - Scan index after writes?

## Recommendation

**Focus on Level 0 first** - build the absolute prerequisites:
1. Path parsing (can test independently)
2. Entry serialization (can test independently)
3. Basic log file I/O (can test with serialization)

Once we have these, we can build up to transaction buffers, index integration, and read operations.

**Missing piece**: We have the schema (top-down), but we need the actual operations (bottom-up) to make it work.
