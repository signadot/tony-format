# Lock Ordering and Deadlock Analysis

## Current Lock Ordering

### Path 1: Storage.commit() (normal commit path)
```
1. indexMu.Lock()           [line 146]
2. index.Add(seg)
3. indexMu.Unlock()         [line 154]
4. compactor.OnNewSegment()  [line 159] - non-blocking, sends to channel
```

**Key:** `OnNewSegment()` is called AFTER releasing `indexMu` to avoid deadlock.

### Path 2: DirCompactor.persistCurrent() (compaction path)
```
1. env.seq.NextTxSeq()      [line 289] - acquires/releases seq lock internally
2. env.seq.Lock()           [line 334]
3. env.seq.NextCommitLocked() [line 336] - while holding seq lock
4. env.seq.Unlock()         [line 335 defer]
5. env.idxL.Lock()          [line 412] - idxL is same as indexMu
6. env.idx.Add(seg)
7. env.idxL.Unlock()        [line 413 defer]
```

**Key:** Compaction holds `seq` lock, then `idxL` (indexMu) lock. This is safe because:
- `commit()` releases `indexMu` before calling `OnNewSegment()`
- Compaction goroutine doesn't hold `indexMu` when receiving from channel

## Proposed Change: OnCommit Callback

### Option A: Call callback WITHIN NextCommitLocked() (WRONG - DEADLOCK RISK)
```go
func (s *Seq) NextCommitLocked() (int64, error) {
    // ... increment commit ...
    commit := state.Commit
    if s.onCommit != nil {
        s.onCommit(commit)  // ⚠️ CALLED WITH seq LOCK HELD
    }
    return commit, nil
}
```

**Problem:** If `OnCommit()` tries to trigger compaction synchronously, and compaction needs `seq` lock (for `NextCommitLocked()` in `persistCurrent()`), we get deadlock:
- Thread 1: Holds `seq` lock, calls `OnCommit()`, waits for compaction
- Thread 2: Compaction goroutine needs `seq` lock for `persistCurrent()`, waits forever

### Option B: Call callback AFTER releasing seq lock (CORRECT)
```go
func (s *Seq) NextCommitLocked() (int64, error) {
    // ... increment commit ...
    commit := state.Commit
    return commit, nil
}

func (s *Seq) NextCommit() (int64, error) {
    s.Lock()
    commit, err := s.NextCommitLocked()
    s.Unlock()
    
    // Call callback AFTER releasing lock
    if s.onCommit != nil {
        s.onCommit(commit)  // ✅ CALLED WITHOUT seq LOCK
    }
    return commit, err
}
```

**But wait:** `persistCurrent()` calls `NextCommitLocked()` directly, not `NextCommit()`. So the callback wouldn't be called!

### Option C: Call callback from Storage.commit() (BEST)
```go
func (s *Storage) commit(virtualPath string, txSeq, commitCount int64) error {
    // ... existing commit logic ...
    
    // After commit is allocated (commitCount is known)
    if s.compactor != nil {
        // Notify of new segment (existing)
        if err := s.compactor.OnNewSegment(seg); err != nil {
            // ...
        }
        // Notify of commit (NEW)
        s.compactor.OnCommit(commitCount)  // ✅ Called without any locks held
    }
    return nil
}
```

**Advantages:**
- No lock held when calling `OnCommit()`
- Commit number is already known
- Consistent with existing pattern (OnNewSegment called after releasing locks)

## Lock Ordering Verification

### Scenario 1: Normal commit + compaction trigger
```
Thread 1 (commit):
  1. indexMu.Lock()
  2. index.Add()
  3. indexMu.Unlock()
  4. OnNewSegment() - sends segment to channel
  5. OnCommit() - checks alignment, sends trigger to channel
  
Thread 2 (compaction goroutine):
  1. Receives segment from channel (no locks)
  2. Receives trigger from channel (no locks)
  3. processSegment() - no locks
  4. persistCurrent():
     a. seq.Lock()
     b. seq.NextCommitLocked() - allocates NEW commit
     c. seq.Unlock()
     d. idxL.Lock() (indexMu)
     e. idx.Add()
     f. idxL.Unlock()
```

**Analysis:** No deadlock. Thread 1 releases all locks before notifying compactor. Thread 2 acquires locks independently.

### Scenario 2: Multiple commits happening concurrently
```
Thread 1 (commit A):
  1. indexMu.Lock()
  2. index.Add(segA)
  3. indexMu.Unlock()
  4. OnCommit(commitA) - checks alignment
  
Thread 2 (commit B):
  1. indexMu.Lock() - waits for Thread 1
  2. ... (continues after Thread 1 releases)
```

**Analysis:** Fine. `indexMu` serializes commits. `OnCommit()` is called without locks, so it doesn't block other commits.

### Scenario 3: Compaction happening during commit
```
Thread 1 (commit):
  1. indexMu.Lock()
  2. index.Add()
  3. indexMu.Unlock()
  4. OnCommit() - triggers compaction check
  
Thread 2 (compaction):
  1. persistCurrent() running:
     a. seq.Lock() - held
     b. seq.NextCommitLocked() - allocating commit
     c. seq.Unlock() - releases
     d. idxL.Lock() - waits if Thread 1 still has it
```

**Analysis:** Fine. Compaction releases `seq` lock before acquiring `idxL`. Commit releases `idxL` before calling `OnCommit()`.

## Sequencing Guarantees

### Question: Can OnCommit() see commits out of order?

**Answer:** YES. Commits are allocated sequentially (due to `seq` lock), but `OnCommit()` calls can happen out of order:
1. `NextCommitLocked()` increments commit atomically (with `seq` lock) - **serialized**
2. `commit()` acquires `indexMu`, updates index, releases `indexMu` - **serialized**
3. `commit()` calls `OnCommit()` **after releasing `indexMu`** - **NOT serialized**

**Example race:**
- Thread 1: `NextCommit()` → commit 1, then `commit()` acquires `indexMu`, updates, releases, calls `OnCommit(1)`
- Thread 2: `NextCommit()` → commit 2, then `commit()` acquires `indexMu`, updates, releases, calls `OnCommit(2)`
- If Thread 2's `commit()` finishes faster, `OnCommit(2)` can be called before `OnCommit(1)`

**Guarantee:** `OnCommit(N)` is called exactly once for commit N, but calls can be out of order relative to commit allocation order.

**Impact:** Compaction must handle out-of-order `OnCommit()` calls correctly.

### Question: Can compaction trigger before segment is in index?

**Answer:** No. Order is:
1. `commit()` adds segment to index
2. `commit()` releases `indexMu`
3. `commit()` calls `OnNewSegment()` (sends segment to compactor)
4. `commit()` calls `OnCommit()` (checks alignment)

So segment is in index before compactor sees it. Compaction can safely read from index.

### Question: Can OnCommit() trigger compaction for a commit that's already being compacted?

**Answer:** Yes, and must handle out-of-order calls:
- `OnCommit(N)` checks if `N % (divisor^level) == 0`
- If yes, sends trigger to compaction goroutine
- Compaction goroutine checks `Inputs >= Divisor` and `CurSegment.EndCommit < N` (strictly less)
- If `CurSegment.EndCommit >= N`, compaction already happened or is in progress, so check fails
- If `CurSegment.EndCommit < N`, compaction proceeds

**Out-of-order handling:**
- If `OnCommit(2)` arrives before `OnCommit(1)`:
  - Commit 2 triggers compaction (if alignment point)
  - Compaction processes segments up to commit 2
  - Commit 1's trigger arrives later, sees `CurSegment.EndCommit >= 1`, skips compaction
- This is safe because segments are sent via `OnNewSegment()` BEFORE `OnCommit()` is called
- So by the time `OnCommit(2)` is called, segment 2 is already in compactor's channel

**Guarantee:** Compaction is idempotent - checking alignment multiple times is safe. Out-of-order `OnCommit()` calls are handled correctly.

## Implementation Details

### OnCommit() Implementation
```go
func (c *Compactor) OnCommit(commit int64) {
    // Check each level for alignment
    for level := 0; level < maxReasonableLevel; level++ {
        align := pow(c.Config.Divisor, level)
        if commit % align == 0 {
            // Trigger compaction check for all paths at this level
            c.triggerCompactionAtLevel(level, commit)
        }
    }
}

func (c *Compactor) triggerCompactionAtLevel(level int, commit int64) {
    c.dcMu.Lock()
    defer c.dcMu.Unlock()
    for _, dc := range c.dcMap {
        if dc.Level == level {
            // Send trigger to compaction goroutine (non-blocking)
            select {
            case dc.compactionTrigger <- commit:
            default:
                // Channel full - compaction will check on next segment
            }
        }
    }
}
```

**Key:** Trigger is sent asynchronously to compaction goroutine. No locks held, no blocking.

### DirCompactor Changes
```go
type DirCompactor struct {
    // ... existing fields ...
    compactionTrigger chan int64  // NEW: receives commit numbers to check
}

func (dc *DirCompactor) run(env *storageEnv) {
    // ... recovery ...
    for {
        select {
        case seg, ok := <-dc.incoming:
            // ... process segment ...
        case commit := <-dc.compactionTrigger:
            // Check if we should compact at this commit
            if dc.shouldCompactAt(commit) {
                // Trigger compaction
            }
        case <-dc.done:
            return
        }
    }
}

func (dc *DirCompactor) shouldCompactAt(commit int64) bool {
    align := pow(dc.Config.Divisor, dc.Level)
    if commit % align != 0 {
        return false  // Not an alignment point
    }
    if dc.Inputs < dc.Config.Divisor {
        return false  // Not enough segments
    }
    // Use strictly less than to handle out-of-order OnCommit() calls:
    // - If OnCommit(2) arrives first and compacts, CurSegment.EndCommit becomes 2
    // - When OnCommit(1) arrives later, CurSegment.EndCommit < 1 is false, so skip
    if dc.CurSegment == nil || dc.CurSegment.EndCommit >= commit {
        return false  // Already compacted to or past this commit
    }
    return true
}
```

## Test Analysis

### TestReadState_BasicHappyPath
**What it tests:** Recovery logic - reads files from disk, reconstructs state
**Assumptions:** None about triggering - just reads what's on disk
**Impact of change:** ✅ **NONE** - Recovery is independent of triggering mechanism

### TestCompactionLogic
**What it tests:** Compaction happens when 2 segments arrive (divisor=2)
**Assumptions:** 
- Creates segments at commits 1, 2
- Expects compaction to trigger immediately after 2nd segment
- Waits for compacted segment to appear in index
**Impact of change:** ⚠️ **NEEDS UPDATE**
- With commit-driven: Compaction triggers at commit 2 (alignment point)
- Test creates segments at commits 1, 2 - commit 2 IS an alignment point for divisor=2
- **Should still work**, but timing may differ (compaction happens at commit 2, not immediately after segment 2)

### TestFileRemoval / TestFileRemovalDisabled
**What it tests:** File removal behavior after compaction
**Assumptions:** Compaction happens (when/how doesn't matter)
**Impact of change:** ✅ **MINIMAL** - May need timing adjustments (wait for alignment point)

### TestHeadWindowStrategy
**What it tests:** Removal strategy based on commit window
**Assumptions:**
- Creates segments at commits 1, 2, 3, 4
- Expects compaction at commits 2 and 4 (every 2 commits)
**Impact of change:** ✅ **SHOULD WORK** - Commits 2 and 4 are alignment points for divisor=2

### Key Test Pattern
All tests use `createSegment()` which:
1. Writes file to disk
2. Calls `compactor.OnNewSegment(seg)` - sends segment to compactor
3. Waits for compaction to appear in index

**With commit-driven compaction:**
- Segments still sent via `OnNewSegment()` (unchanged)
- Compaction triggers at alignment points (new)
- Tests that create segments at alignment points (2, 4, 6...) will work
- Tests that create segments at non-alignment points (1, 3, 5...) may need updates

## Conclusion

**Lock Ordering:** ✅ Safe
- `OnCommit()` called without locks held
- Compaction acquires locks independently
- No circular dependencies

**Sequencing:** ✅ Correct
- Commits allocated sequentially
- `OnCommit()` called after commit allocated
- Segment in index before compactor sees it

**Deadlocks:** ✅ None
- All callbacks non-blocking
- Locks released before callbacks
- Compaction doesn't hold locks when receiving triggers

**Tests:** ⚠️ **Mostly compatible**
- Recovery tests: No changes needed
- Compaction tests: May need timing adjustments
- File removal tests: Should work with minor timing changes
- Tests creating segments at alignment points (2, 4, 6...) will work as-is
- Tests creating segments at non-alignment points may need to wait for next alignment

**Implementation:** ✅ Feasible
- Minimal changes to sequencer (add callback field)
- Callback called from `Storage.commit()` (after locks released)
- Compaction checks alignment asynchronously
- Tests mostly compatible with minor timing adjustments
