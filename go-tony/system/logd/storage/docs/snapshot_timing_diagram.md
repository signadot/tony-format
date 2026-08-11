# Snapshot Timing and Index Interaction

## Timeline: Double-Buffer with Delayed Snapshot

```
t0: logA active
    Write commit 1 → Index.Add(Patch[0,1], logA, pos0)
    Write commit 2 → Index.Add(Patch[1,2], logA, pos1)
    Write commit 3 → Index.Add(Patch[2,3], logA, pos2)

    Index state: Tree { Patch[0,1], Patch[1,2], Patch[2,3] }

t1: Switch to logB
    SwitchActive() → logB becomes active
    Start async: Builder.Build(logA) → snapshot for commits 1-3

    Index state: Tree { Patch[0,1], Patch[1,2], Patch[2,3] }

t2: logB active (snapshot still building)
    Write commit 4 → Index.Add(Patch[3,4], logB, pos0)
    Write commit 5 → Index.Add(Patch[4,5], logB, pos1)

    Index state: Tree { Patch[0,1], Patch[1,2], Patch[2,3], Patch[3,4], Patch[4,5] }

t3: Snapshot completes (AFTER commit 5 written)
    Index.Add(Snapshot[3,3], logA, snap_pos)

    Index state: Tree { Patch[0,1], Patch[1,2], Patch[2,3], Snapshot[3,3], Patch[3,4], Patch[4,5] }
                         ^                                    ^
                         |                                    |
                    Order by StartCommit/StartTx      Inserted late, placed correctly
```

## Index Tree Ordering

Tree maintains order by: `(StartCommit, StartTx, EndCommit, EndTx, KindedPath)`

After snapshot insertion at t3:
```
Patch[0,1]      StartCommit=0, StartTx=1, EndCommit=1
Patch[1,2]      StartCommit=1, StartTx=2, EndCommit=2
Patch[2,3]      StartCommit=2, StartTx=3, EndCommit=3
Snapshot[3,3]   StartCommit=3, StartTx=3, EndCommit=3  ← inserted late, but ordered here
Patch[3,4]      StartCommit=3, StartTx=4, EndCommit=4
Patch[4,5]      StartCommit=4, StartTx=5, EndCommit=5
```

Tree structure handles out-of-order insertion automatically.

## Query Methods

### LookupRange(kp, &from, &to) - CORRECT for ReadStateAt

Returns segments where: `from <= StartCommit <= to`

**Query: LookupRange("", &zero, &five)**
```
Patch[0,1]      0 <= 0 <= 5? YES ✓
Patch[1,2]      0 <= 1 <= 5? YES ✓
Patch[2,3]      0 <= 2 <= 5? YES ✓
Snapshot[3,3]   0 <= 3 <= 5? YES ✓
Patch[3,4]      0 <= 3 <= 5? YES ✓
Patch[4,5]      0 <= 4 <= 5? YES ✓

Returns: ALL segments needed to reconstruct state at commit 5
Sorted by: StartCommit, StartTx, EndCommit, EndTx, KindedPath
```

### LookupWithin(kp, commit) - WRONG for ReadStateAt

Returns segments where: `StartCommit <= commit <= EndCommit`

**Query: LookupWithin("", 5)**
```
Patch[0,1]      0 <= 5 <= 1? NO ✗
Patch[1,2]      1 <= 5 <= 2? NO ✗
Patch[2,3]      2 <= 5 <= 3? NO ✗
Snapshot[3,3]   3 <= 5 <= 3? NO ✗
Patch[3,4]      3 <= 5 <= 4? NO ✗
Patch[4,5]      4 <= 5 <= 5? YES ✓

Returns: Only the LAST patch [4,5]
Cannot reconstruct full state!
```

## Corrected ReadStateAt Flow

```go
func ReadStateAt(kp string, commit int64) (*ir.Node, error) {
    // Get ALL patches from 0 to commit
    zero := int64(0)
    segments := index.LookupRange(kp, &zero, &commit)

    // Sort by StartCommit, StartTx (already done by LookupRange)

    // Optimization: Find latest snapshot <= commit
    var state *ir.Node
    startFrom := 0
    for i, seg := range segments {
        if seg.StartCommit == seg.EndCommit {  // Snapshot
            // Start from this snapshot instead of null
            state = LoadSnapshot(seg)
            startFrom = i + 1
            break
        }
    }

    if state == nil {
        state = ir.Null()
    }

    // Apply remaining patches
    for i := startFrom; i < len(segments); i++ {
        patch := LoadPatch(segments[i])
        state = tony.Patch(state, patch)
    }

    return state, nil
}
```

## Non-Monotonicity Impact: NONE

**Key insights:**

1. **Tree ordering is by StartCommit/StartTx, not insertion time**
   - Snapshot[3,3] inserted at t3 is placed at correct position in tree
   - Query results are always in correct commit order

2. **Snapshots are optimization, not requirement**
   - System works correctly with patches only
   - Snapshot arrival just enables faster queries (skip early patches)

3. **No monotonicity assumption needed**
   - Index.Add() can be called in any order
   - Tree maintains logical ordering by commit number
   - Queries return segments in commit order

4. **Late snapshot is safe**
   - Between t2 and t3: queries use patches only (correct but slower)
   - After t3: queries can use snapshot (correct and faster)
   - No correctness issue, only performance optimization

## Conclusion

The double-buffer non-monotonicity is **architecturally sound**. The Tree-based index
handles out-of-order insertions correctly. The only bug is that ReadStateAt currently
uses LookupWithin instead of LookupRange, which prevents it from reconstructing state
at all (completely broken, unrelated to snapshot timing).

**Required fix:** Change ReadStateAt to use LookupRange.

**Future optimization:** Detect snapshots in query results and skip earlier patches.
