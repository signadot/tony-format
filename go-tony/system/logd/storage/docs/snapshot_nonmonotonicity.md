# Snapshot Non-Monotonicity in Double-Buffered Logging

## The Problem

Double-buffering creates a situation where snapshot creation completes **after** subsequent commits are written, leading to non-monotonic index updates.

## Scenario

```
Timeline:

t0: logA active
    - Write commit 1 → index: [Patch(0→1, logA, pos0)]
    - Write commit 2 → index: [Patch(0→1), Patch(1→2, logA, pos1)]
    - Write commit 3 → index: [Patch(0→1), Patch(1→2), Patch(2→3, logA, pos2)]

t1: Switch to logB (logA becomes inactive)
    - Start async snapshot creation from logA (covers commits 1-3)
    - logB now active

t2: logB active
    - Write commit 4 → index: [... Patch(2→3), Patch(3→4, logB, pos0)]
    - Write commit 5 → index: [... Patch(3→4), Patch(4→5, logB, pos1)]

t3: Snapshot creation completes (AFTER commit 5 written)
    - Add snapshot → index: [... Patch(4→5), Snapshot(3, logA, snap_pos)]

Result: Index entry for commit 3 snapshot appears AFTER commit 5 entry
```

## LogSegment Semantics

From `index/log_segment.go`:

```go
type LogSegment struct {
    StartCommit   int64  // For patch: LastCommit, For snapshot: commit
    EndCommit     int64  // For patch: Commit, For snapshot: commit (same)
    KindedPath    string
    LogFile       string
    LogPosition   int64
}

// Semantics:
// - StartCommit == EndCommit: snapshot (full state at that commit)
// - StartCommit != EndCommit: diff (incremental changes over commit range)
```

**Examples:**
- Patch at commit 3 (LastCommit=2): `{Start:2, End:3, ...}` → covers transition 2→3
- Snapshot at commit 3: `{Start:3, End:3, ...}` → full state at commit 3

## Index Query: LookupWithin

```go
func (i *Index) LookupWithin(kp string, commit int64) []LogSegment {
    // Returns segments where: StartCommit <= commit && commit <= EndCommit
}
```

**For commit 3:**
- Patch covering [2,3]: `2 <= 3 && 3 <= 3` → ✓ included
- Patch covering [1,2]: `1 <= 3 && 3 <= 2` → ✗ excluded
- Snapshot at [3,3]: `3 <= 3 && 3 <= 3` → ✓ included

**Issue:** This doesn't return ALL patches needed to reconstruct state at commit 3.

Need to verify: Does ReadStateAt call LookupRange instead?

## Critical Bug: ReadStateAt Uses Wrong Query

**ReadStateAt currently uses LookupWithin - this is WRONG.**

`LookupWithin(kp, commit)` returns segments where `StartCommit <= commit <= EndCommit`.
This is a **point query** - finds segments that "contain" the commit.

**Example: Query at commit 5**
- Patch [0,1]: 0 <= 5 && 5 <= 1? NO
- Patch [1,2]: 1 <= 5 && 5 <= 2? NO
- Patch [2,3]: 2 <= 5 && 5 <= 3? NO
- Patch [3,4]: 3 <= 5 && 5 <= 4? NO
- Patch [4,5]: 4 <= 5 && 5 <= 5? YES ← **Only this one!**

Result: Gets only the LAST patch [4,5], not the cumulative history.

**Correct Approach:**

ReadStateAt should use `LookupRange(kp, &zero, &commit)` to get ALL segments
with StartCommit in range [0, commit]. This returns the full patch sequence
needed for reconstruction.

**Impact on Non-Monotonicity:**

Once ReadStateAt is fixed to use LookupRange:
1. Tree ordering by StartCommit ensures correct sequence regardless of insertion order
2. Delayed snapshot insertion is safe - Tree places it at correct position
3. Query returns segments in correct order: Patch[0,1], Patch[1,2], Patch[2,3], Snapshot[3,3], ...
4. ReadStateAt can prefer snapshot over replaying patches (optimization)

## Remaining Questions

1. **Snapshot optimization strategy:**
   - When query finds Snapshot[N,N] in range, skip all patches before N
   - Apply patches after N to get to target commit
   - Requires sorting results and detecting snapshot presence

2. **Index ordering assumptions:**
   - Tree structure ordered by StartCommit/StartTx - out-of-order inserts handled ✓
   - LogSegCompare provides total ordering across all fields
   - No monotonic insertion requirement ✓

3. **Consistency guarantees:**
   - Snapshot represents redundant information (optimization, not required)
   - Missing snapshot → fall back to patches (always valid)
   - Late snapshot arrival → Tree handles ordering correctly
   - No gaps possible - patches form continuous sequence by construction

## Proposed Investigation

1. Trace ReadStateAt → understand if it uses LookupWithin or LookupRange
2. Check if LookupRange returns cumulative patches
3. Verify index Tree handles out-of-order inserts correctly
4. Design snapshot integration that handles delayed availability

## Key Insight

**Snapshots don't add information - they optimize access.**

This means:
- System must work correctly with patches only
- Snapshots are pure optimization
- Late snapshot arrival is annoying but not incorrect
- Need to ensure ReadStateAt doesn't REQUIRE snapshots
