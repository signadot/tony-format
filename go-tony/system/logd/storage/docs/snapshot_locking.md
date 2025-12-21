# Snapshot Creation Locking Analysis

## Problem

Snapshot creation has two potential concurrency issues:

1. **Deadlock in WriteSnapshotToInactive**: Calling `GetInactiveLog()` while holding `dl.mu` would try to acquire `dl.mu.RLock()` - deadlock.

2. **Race condition in SwitchAndSnapshot**: Without coordination, the following sequence could happen:
   ```
   Thread A: SwitchActive() → logA becomes inactive
   Thread A: CreateSnapshot(logA) starts (slow operation)
   Thread B: SwitchActive() → logA becomes active again!
   Thread A: Still writing snapshot to logA
   Thread B: New commits append to logA
   Result: Snapshot data interleaved with new commits
   ```

## Solution

### Fix 1: Remove Deadlock in WriteSnapshotToInactive

**Before:**
```go
func (dl *DLog) WriteSnapshotToInactive(...) {
    dl.mu.Lock()
    inactiveLog := dl.GetInactiveLog()  // Tries to acquire dl.mu.RLock() - DEADLOCK
    dl.mu.Unlock()
    // ...
}
```

**After:**
```go
func (dl *DLog) WriteSnapshotToInactive(...) {
    dl.mu.RLock()
    activeLog := dl.activeLog  // Read directly, no nested lock
    dl.mu.RUnlock()

    // Determine inactive log from active log
    var inactiveLog LogFileID
    if activeLog == LogFileA {
        inactiveLog = LogFileB
    } else {
        inactiveLog = LogFileA
    }
    // ...
}
```

### Fix 2: Add switchMu to Storage

**Added to Storage struct:**
```go
type Storage struct {
    // ...
    switchMu sync.Mutex  // Protects log switching to prevent concurrent switch+snapshot
}
```

**Protected SwitchAndSnapshot:**
```go
func (s *Storage) SwitchAndSnapshot() error {
    s.switchMu.Lock()
    defer s.switchMu.Unlock()

    commit := s.GetCurrentCommit()
    s.dLog.SwitchActive()
    s.CreateSnapshot(commit)  // Long operation protected by switchMu

    return nil
}
```

## Locking Guarantees

### DLog Level

**Locks:** `dl.mu` (protects activeLog), `logFileObj.mu` (protects file operations)

**AppendEntry:**
```go
dl.mu.Lock()
activeLog = dl.activeLog
dl.mu.Unlock()

logFileObj.mu.Lock()
// Write to file
logFileObj.mu.Unlock()
```
- Acquires `dl.mu` briefly to read active log
- Acquires `logFileObj.mu` for file write
- No nested locking

**WriteSnapshotToInactive:**
```go
dl.mu.RLock()
activeLog = dl.activeLog
dl.mu.RUnlock()

logFileObj.mu.Lock()  // logFileObj is INACTIVE log
// Write events + entry to file
logFileObj.mu.Unlock()
```
- Acquires `dl.mu.RLock()` briefly to read active log
- Acquires `logFileObj.mu` for inactive log (never conflicts with AppendEntry)
- No nested locking

**SwitchActive:**
```go
dl.mu.Lock()
dl.activeLog = flip(dl.activeLog)
dl.mu.Unlock()
```
- Acquires `dl.mu` briefly to flip active log
- No file lock needed

**Key property:** DLog-level locking is always brief, never holds locks during I/O.

### Storage Level

**Locks:** `s.switchMu` (protects switching operations)

**SwitchAndSnapshot:**
```go
s.switchMu.Lock()
defer s.switchMu.Unlock()

commit = GetCurrentCommit()
SwitchActive()         // Brief: just flips activeLog
CreateSnapshot()       // Long: reads all patches, writes events
```

**Guarantees:**
1. Only one SwitchAndSnapshot can run at a time
2. Snapshot creation completes before another switch can occur
3. No interleaving of snapshot data with commits

**Why this works:**
- `switchMu` serializes ALL switch operations
- During snapshot creation, inactive log remains inactive
- New commits always go to the active log (never touched by snapshot)
- When snapshot completes, switchMu is released

## Deadlock Prevention

**Rule:** Never hold multiple locks simultaneously in the same call chain.

**Verified:**
- ✓ `WriteSnapshotToInactive`: Holds only one lock at a time (dl.mu.RLock briefly, then logFileObj.mu)
- ✓ `AppendEntry`: Holds only one lock at a time (dl.mu briefly, then logFileObj.mu)
- ✓ `SwitchActive`: Holds only dl.mu briefly
- ✓ `SwitchAndSnapshot`: Holds switchMu, but all called methods release their locks before returning

## Correctness

**Invariant:** Inactive log is never written to by AppendEntry.

**Proof:**
1. `AppendEntry` reads `dl.activeLog`, writes to that log
2. `WriteSnapshotToInactive` reads `dl.activeLog`, writes to opposite log
3. At any instant, `activeLog` has one value (A or B)
4. Therefore, AppendEntry and WriteSnapshotToInactive target different files
5. `logFileObj.mu` for each file protects against concurrent writes to same file
6. Since they target different files, no conflict possible

**Invariant:** Snapshot completes before log switches back.

**Proof:**
1. `SwitchAndSnapshot` acquires `switchMu` at start
2. Performs switch + snapshot creation while holding `switchMu`
3. Releases `switchMu` only after snapshot completes
4. Another `SwitchAndSnapshot` must wait for `switchMu`
5. Therefore, snapshot completes before next switch can occur

## Test Coverage

`TestSwitchAndSnapshot` verifies:
- Switch operation succeeds
- Snapshot entry created in index
- Snapshot entry has correct commit, SnapPos, logFile
- Entry readable from inactive log
- No deadlock or timeout
