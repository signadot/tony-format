# Commit-Driven Compaction: Design Justification

## Executive Summary

**Proposal:** Transform compaction from segment-count-driven to commit-alignment-driven.

**Status:** ✅ **SAFE** - Lock ordering verified, no deadlocks, sequencing correct, tests mostly compatible.

## Detailed Justification

### 1. Lock Ordering Analysis

**Current Ordering:**
- `Storage.commit()`: `indexMu` → unlock → `OnNewSegment()` (no locks)
- `persistCurrent()`: `seq` → unlock → `idxL` (indexMu)

**Proposed Change:**
- Add `OnCommit(commit)` callback called from `Storage.commit()` AFTER releasing `indexMu`
- Same pattern as `OnNewSegment()` - called without locks held

**Verification:**
- ✅ No locks held when calling `OnCommit()`
- ✅ Compaction acquires locks independently
- ✅ No circular dependencies

**Conclusion:** Lock ordering is safe. No deadlock risk.

### 2. Sequencing Guarantees

**Question:** Can `OnCommit()` see commits out of order?

**Answer:** YES. Commits are allocated sequentially (due to `seq` lock), but `OnCommit()` calls can happen out of order:
1. `NextCommitLocked()` increments commit atomically (with `seq` lock) - **serialized**
2. `commit()` acquires `indexMu`, updates index, releases `indexMu` - **serialized**  
3. `commit()` calls `OnCommit()` **after releasing `indexMu`** - **NOT serialized**

**Example:** If commit 2's `commit()` finishes faster than commit 1's, `OnCommit(2)` can be called before `OnCommit(1)`.

**Guarantee:** `OnCommit(N)` is called exactly once for commit N, but calls can be out of order relative to commit allocation order.

**Handling:** Compaction checks `CurSegment.EndCommit < commit` (strictly less), so out-of-order calls are safe:
- If `OnCommit(2)` arrives first and compacts, `CurSegment.EndCommit` becomes 2
- When `OnCommit(1)` arrives later, check `CurSegment.EndCommit < 1` fails, so compaction skipped
- This is safe because segments are sent via `OnNewSegment()` BEFORE `OnCommit()` is called

**Question:** Can compaction trigger before segment is in index?

**Answer:** No. Order is:
1. `commit()` adds segment to index
2. `commit()` releases `indexMu`
3. `commit()` calls `OnNewSegment()` (sends segment to compactor)
4. `commit()` calls `OnCommit()` (checks alignment)

Segment is in index before compactor sees it. Compaction can safely read from index.

**Question:** Can `OnCommit()` trigger compaction for a commit that's already being compacted?

**Answer:** Possibly, but safe:
- Compaction checks `CurSegment.EndCommit <= commit`
- If already compacted, check fails (idempotent)
- Multiple triggers for same commit are harmless

**Conclusion:** Sequencing handles out-of-order `OnCommit()` calls correctly. No race conditions.

### 3. Deadlock Analysis

**Scenario 1: Normal commit + compaction**
- Thread 1 (commit): Releases all locks before calling `OnCommit()`
- Thread 2 (compaction): Acquires locks independently
- **Result:** ✅ No deadlock

**Scenario 2: Multiple commits**
- `indexMu` serializes commits
- `OnCommit()` called without locks
- **Result:** ✅ No deadlock

**Scenario 3: Compaction during commit**
- Compaction releases `seq` lock before acquiring `idxL`
- Commit releases `idxL` before calling `OnCommit()`
- **Result:** ✅ No deadlock

**Conclusion:** No deadlock scenarios identified.

### 4. Test Compatibility

**Tests that work as-is:**
- ✅ `TestReadState_BasicHappyPath` - Recovery logic (independent of triggering)
- ✅ Tests creating segments at alignment points (2, 4, 6... for divisor=2)

**Tests needing minor updates:**
- ⚠️ `TestCompactionLogic` - May need timing adjustments (wait for alignment point)
- ⚠️ `TestFileRemoval` - May need timing adjustments
- ⚠️ `TestHeadWindowStrategy` - Should work (uses commits 2, 4 which are alignment points)

**Key Insight:** Tests that create segments at alignment points will work. Tests creating segments at non-alignment points may need to wait for next alignment.

**Conclusion:** Tests mostly compatible with minor timing adjustments.

### 5. Implementation Feasibility

**Changes Required:**

1. **Sequencer** (`seq/sequence.go`):
   - Add `onCommit func(int64)` field
   - Add `SetOnCommitCallback(fn func(int64))` method
   - **No lock changes** - callback called from `Storage.commit()` without locks

2. **Compactor** (`compact/compact.go`):
   - Add `OnCommit(commit int64)` method
   - Check alignment: `commit % (divisor^level) == 0`
   - Send trigger to compaction goroutines (non-blocking)

3. **DirCompactor** (`compact/dir_compact.go`):
   - Add `compactionTrigger chan int64` field
   - Remove `return dc.Inputs >= dc.Config.Divisor` from `addSegment()` (keep counter, don't trigger)
   - Add `shouldCompactAt(commit int64) bool` method
   - Add trigger handling in `run()` loop

4. **Storage** (`storage.go`):
   - Call `compactor.OnCommit(commitCount)` after `OnNewSegment()` in `commit()`

**Complexity:** ~150 lines of code changes, mostly additions.

**Risk:** Low - follows existing patterns, no lock changes, non-blocking callbacks.

**Conclusion:** Implementation is feasible and low-risk.

## Final Verdict

❌ **REJECTED** - Design is too fragile and relies on subtle timing assumptions.

**Issues:**
1. `OnCommit()` calls can arrive out of order, creating complex synchronization requirements
2. Explanation relies on "segments arrive before OnCommit()" which is a timing assumption, not a guarantee
3. Handling out-of-order calls requires convoluted logic that will be hard to maintain
4. Risk of subtle bugs that will be difficult to debug later

**Recommendation:**
See `design_alternatives.md` for simpler approaches:
- **Option A:** Best-effort alignment (check alignment when compacting, but don't guarantee it)
- **Option B:** Revisit requirement - is perfect alignment necessary?
- **Option C:** If perfect alignment required, consider global compaction architecture

**Next Steps:**
1. Discuss with user: Is perfect alignment required, or is best-effort sufficient?
2. If perfect alignment required, consider architectural changes (global compaction)
3. If best-effort sufficient, implement simpler alignment check in existing compaction logic
