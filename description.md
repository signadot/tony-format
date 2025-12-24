# logd: unnecessary file handle overhead in read path

## Problem

The read path opens/seeks/closes file handles repeatedly when reading multiple entries from the same log file:

1. `ReadStateAt` loops through segments and calls `dLog.ReadEntryAt()` for each
2. `ReadPatchesInRange` similarly reads multiple entries in a loop
3. Each `ReadEntryAt` call opens the file, seeks to position, reads, and closes

For reads requiring many patches (e.g., replaying from an old commit), this results in dozens of open/seek/close cycles on the same file(s).

## Solution Sketch

Create a `ReaderAt` abstraction that maintains one `io.ReaderAt` per log file under the hood:

```go
type MultiLogReader struct {
    readers map[LogFileID]*os.File  // or io.ReaderAt
}

func (r *MultiLogReader) ReadAt(logFile LogFileID, offset int64, p []byte) (int, error) {
    // Get or create reader for this log file
    // Use ReadAt (pread) - no seeking required, thread-safe
}

func (r *MultiLogReader) Close() error {
    // Close all open file handles
}
```

Usage in read loops:

```go
func (s *Storage) ReadStateAt(kp string, commit int64) (*ir.Node, error) {
    reader := s.dLog.NewMultiReader()
    defer reader.Close()
    
    for _, seg := range segments {
        entry, err := reader.ReadEntryAt(seg.LogFile, seg.LogPosition)
        // ...
    }
}
```

Benefits:
- Each log file opened at most once per read operation
- `ReadAt` (pread syscall) is thread-safe and doesn't require explicit seeks
- File handles cleaned up when operation completes