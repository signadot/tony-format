# Conditional PATCH Writes Design

## Problem Statement

The current PATCH implementation doesn't validate the `match` field from requests - it blindly applies patches without checking if the current state matches expected conditions. This prevents optimistic concurrency control.

## Background

### Current Architecture

**Non-TX Flow:**
```
handlePatchData → WriteDiffAtomically → 
  Seq.Lock → allocate commitCount+txSeq → write file → update index
```

**TX Flow:**
```
handlePatchDataWithTransaction → WriteDiffAtomically(pending=true) →
  Seq.Lock → allocate commitCount+txSeq → write .pending file
  (no index update)
  
Later: commitTransaction → CommitPendingDiff →
  rename .pending to .diff → update index
```

### Sequence Tracking Implementation

Added to `storage/seq/sequence.go`:

```go
type State struct {
    CommitCount int64            // Global monotonic commit count
    TxSeq       int64            // Transaction sequence number
    PathSeqs    map[string]int64 // Per-path last commit seq
}

// Conditional commit allocation
func (s *Seq) NextCommitCountIfPathSeqMatches(
    path string,
    expectedSeq *int64,
) (int64, bool, error)

// Query current seq for a path
func (s *Seq) GetPathSeq(path string) (int64, error)
```

Persistence: `meta/path-seqs.tony` in Tony format for human readability.

## Design Options

### Option A: Check Match at Commit Time (TX-Friendly) ⭐ RECOMMENDED

Store the match condition with each pending diff and validate at commit time.

#### Data Structure Changes

```go
type PendingDiff struct {
    Path           string
    DiffFile       string
    WrittenAt      string
    MatchCondition *ir.Node  // NEW: store match for validation at commit
}
```

#### Flow

**Non-TX Write:**
1. Reconstruct current state at path
2. Check match condition against current state
3. If matched: allocate commitCount, write diff, update index
4. If not matched: return error (conflict)

**TX Write:**
1. Store diff + match condition in .pending file
2. Don't check match yet (state may change before commit)
3. Don't allocate commitCount yet

**TX Commit:**
1. For each pending diff in transaction:
   - Reconstruct current state at path
   - Check stored match condition
   - If ANY fail: abort entire transaction
2. If all match: allocate ONE commitCount for all diffs
3. Rename all .pending to .diff with same commitCount
4. Update index

#### Storage Interface

```go
// Non-TX conditional write
func (s *Storage) WriteDiffConditionally(
    path string,
    timestamp string,
    matchCondition *ir.Node,  // nil = unconditional
    diff *ir.Node,
) (commitCount, txSeq int64, matched bool, err error) {
    // Reconstruct state
    state, seq, err := s.reconstructState(path, nil)
    if err != nil {
        return 0, 0, false, err
    }
    
    // Check match if provided
    if matchCondition != nil && matchCondition.Type != ir.NullType {
        matched, err := tony.Match(state, matchCondition)
        if err != nil || !matched {
            return 0, 0, false, err
        }
    }
    
    // Allocate and write atomically
    s.Seq.Lock()
    defer s.Seq.Unlock()
    
    // Re-check seq hasn't changed (double-check locking)
    currentSeq, _ := s.Seq.GetPathSeq(path)
    if currentSeq != seq {
        return 0, 0, false, nil // Conflict
    }
    
    // Allocate commit and update path seq
    state, _ := s.Seq.ReadStateLocked()
    state.CommitCount++
    state.TxSeq++
    if state.PathSeqs == nil {
        state.PathSeqs = make(map[string]int64)
    }
    state.PathSeqs[path] = state.CommitCount
    
    // Write diff and seq state
    s.writeDiffLocked(path, state.CommitCount, state.TxSeq, timestamp, diff, false)
    s.Seq.WriteStateLocked(state)
    
    // Update index
    s.indexMu.Lock()
    s.index.Add(logSeg)
    s.indexMu.Unlock()
    
    return state.CommitCount, state.TxSeq, true, nil
}

// TX write - store match condition for later validation
func (s *Storage) WriteDiffPending(
    path string,
    timestamp string,
    matchCondition *ir.Node,
    diff *ir.Node,
) (txSeq int64, err error) {
    // Allocate txSeq only (not commitCount)
    txSeq, err = s.Seq.NextTxSeq()
    if err != nil {
        return 0, err
    }
    
    // Write .pending file with match condition embedded
    // (or store in PendingDiff in TransactionState)
    ...
}

// TX commit - validate all matches before committing
func (s *Storage) CommitTransaction(
    txID string,
    state *TransactionState,
) (commitCount int64, err error) {
    // Validate all match conditions
    for _, pendingDiff := range state.Diffs {
        if pendingDiff.MatchCondition != nil {
            currentState, _, err := s.reconstructState(pendingDiff.Path, nil)
            if err != nil {
                return 0, err
            }
            
            matched, err := tony.Match(currentState, pendingDiff.MatchCondition)
            if err != nil || !matched {
                return 0, fmt.Errorf("match failed for path %s", pendingDiff.Path)
            }
        }
    }
    
    // All matches passed - allocate ONE commitCount
    commitCount, err = s.Seq.NextCommitCount()
    if err != nil {
        return 0, err
    }
    
    // Commit all pending diffs with same commitCount
    for _, pendingDiff := range state.Diffs {
        s.CommitPendingDiff(pendingDiff.Path, pendingDiff.TxSeq, commitCount)
    }
    
    return commitCount, nil
}
```

#### Pros
- ✅ Correct semantics for both TX and non-TX
- ✅ Full match condition support (not just seq-based)
- ✅ Compaction makes state reconstruction fast
- ✅ Consistent behavior across TX and non-TX

#### Cons
- ❌ More complex commit logic
- ❌ Need state reconstruction at commit time
- ❌ Match conditions stored in transaction state

---

### Option B: Separate Conditional from Unconditional

Only support conditional writes for non-TX, keep TX simple.

```go
// For non-TX conditional writes
func (s *Storage) WriteDiffConditionally(
    path string,
    timestamp string,
    expectedSeq *int64,
    diff *ir.Node,
) (commitCount, txSeq int64, matched bool, err error) {
    commitCount, matched, err = s.Seq.NextCommitCountIfPathSeqMatches(path, expectedSeq)
    if !matched || err != nil {
        return
    }
    // Continue with normal write...
}

// For TX writes - NO conditional support
func (s *Storage) WriteDiffAtomically(..., pending bool) {
    // Existing logic, no seq checking
}
```

#### Pros
- ✅ Simple, clear separation
- ✅ Easy to implement
- ✅ No TX complexity

#### Cons
- ❌ Transactions don't support conditional writes
- ❌ Inconsistent API (TX vs non-TX)
- ❌ Only seq-based, not full match conditions

---

### Option C: Optimistic Lock at Write, Validate at Commit

Store expected seq with pending diffs, validate at commit.

```go
type PendingDiff struct {
    Path         string
    DiffFile     string
    WrittenAt    string
    ExpectedSeq  *int64  // NEW: expected seq at write time
}
```

**TX Flow:**
1. **Pending write**: Record expectedSeq (current path seq)
2. **Commit**: Check all expectedSeqs still match before committing
   - If any changed, abort transaction
   - Otherwise proceed

#### Pros
- ✅ Catches conflicts early
- ✅ Simpler than full match conditions
- ✅ Works with TX

#### Cons
- ❌ Only seq-based, not full match conditions
- ❌ Still need seq tracking
- ❌ Less flexible than Option A

---

## Recommendation

**Option A** is recommended because:

1. **Compaction makes it viable**: State reconstruction will be fast with compacted diffs
2. **Full match support**: Not limited to seq-based checks
3. **Consistent semantics**: TX and non-TX work the same way
4. **Future-proof**: Supports complex match conditions

## Implementation Plan

### Phase 1: Non-TX Conditional Writes
1. Implement `WriteDiffConditionally` with match checking
2. Update `handlePatchData` to use match condition
3. Add tests for conditional writes

### Phase 2: TX Conditional Writes
1. Add `MatchCondition` to `PendingDiff`
2. Update `WriteDiffPending` to store match condition
3. Update `CommitTransaction` to validate all matches
4. Add tests for TX conditional writes

### Phase 3: Optimization
1. Cache reconstructed states during commit validation
2. Optimize match checking for common patterns
3. Add metrics for match failures

## Open Questions

1. **Match condition storage**: Embed in .pending file or in TransactionState?
   - Recommendation: TransactionState (easier to modify)

2. **Partial TX commit**: If one match fails, abort all or just that path?
   - Recommendation: Abort all (atomic semantics)

3. **Match error vs conflict**: Different error codes?
   - Recommendation: Yes - `ErrMatchFailed` vs `ErrMatchError`

4. **Retry logic**: Server-side or client-side?
   - Recommendation: Client-side (server just returns conflict)

## References

- Seq package: `system/logd/storage/seq/sequence.go`
- Storage interface: `system/logd/storage/storage.go`
- PATCH handler: `system/logd/server/patch_data.go`
- Transaction logic: `system/logd/server/patch_transaction.go`
