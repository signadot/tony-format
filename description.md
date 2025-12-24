# logd: index RLock held during disk I/O in findSnapshotBaseReader

In `snap_storage.go:26-27`, `findSnapshotBaseReader` holds the index RLock during the entire iteration, including dlog reads which are disk I/O operations:

```go
s.index.RLock()
defer s.index.RUnlock()

for seg := range iter.CommitsAt(commit, index.Down) {
    // ...
    entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)  // I/O!
    // ...
    snapReader, err := s.dLog.OpenReaderAt(...)  // I/O!
    // ...
}
```

## Problem
- Slow disk reads block all index writers
- Under heavy read load with slow storage, writes can be delayed significantly
- Potential for priority inversion

## Potential Fix
Collect segment info while holding lock, then release lock before doing I/O:
```go
s.index.RLock()
var targetSeg *index.LogSegment
for seg := range iter.CommitsAt(commit, index.Down) {
    if seg.StartCommit == seg.EndCommit {
        segCopy := seg
        targetSeg = &segCopy
        break
    }
}
s.index.RUnlock()

if targetSeg != nil {
    // Do I/O without holding lock
    entry, err := s.dLog.ReadEntryAt(...)
}
```