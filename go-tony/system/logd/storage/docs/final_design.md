# Commit-Driven Compaction: Final Design

## Overview

Transform compaction from segment-count-driven to commit-alignment-driven using a **write-ahead intent pattern**:

1. **Intent Creation:** When commit N is allocated and `N % (divisor^level) == 0`, write a compaction intent atomically (with seq lock held)
2. **Intent Distribution:** Intent notifies all DirCompactors at that level
3. **Compaction Triggering:** DirCompactors check alignment when processing segments, compacting when both conditions are met:
   - `Inputs >= Divisor` (enough segments accumulated)
   - `EndCommit % align == 0` (at alignment point, or intent indicates alignment point)

## Design Principles

1. **Atomic Intent Creation:** Intents written with seq lock held → no out-of-order issues
2. **Coordination:** All paths see same alignment points via intents
3. **Simple Alignment Check:** Deterministic `commit % align == 0` check
4. **No Timing Assumptions:** Segments processed in order, intents written atomically

## Components

### 1. Intent Structure

```go
type CompactionIntent struct {
    Commit    int64  // Commit number (alignment point)
    Level     int    // Compaction level
}
```

**Storage:** In-memory, thread-safe queue in Compactor. Simple to start, can upgrade to persistent later if needed.

### 2. Intent Creation

**Location:** `Seq.NextCommitLocked()` - after commit is allocated, before lock is released

**Logic:**
```go
func (s *Seq) NextCommitLocked() (int64, error) {
    // ... increment commit ...
    commit := state.Commit
    
    if err := s.WriteStateLocked(state); err != nil {
        return 0, err
    }
    
    // Check alignment and write intents (seq lock still held - atomic!)
    if s.onCommit != nil {
        s.onCommit(commit)  // Writes intents atomically
    }
    
    return commit, nil
}
```

**Compactor.OnCommit():**
```go
func (c *Compactor) OnCommit(commit int64) {
    // Called with seq lock held - atomic!
    // Check each level for alignment
    for level := 0; level < maxReasonableLevel; level++ {
        align := pow(c.Config.Divisor, level)
        if commit % align == 0 {
            // Notify all DirCompactors at this level
            c.notifyAlignmentPoint(level, commit)
        }
    }
}
```

### 3. Intent Distribution

**Compactor.notifyAlignmentPoint():**
```go
func (c *Compactor) notifyAlignmentPoint(level int, commit int64) {
    c.dcMu.Lock()
    defer c.dcMu.Unlock()
    
    for _, dc := range c.dcMap {
        if dc.Level == level {
            // Non-blocking send - don't block commit path
            select {
            case dc.alignmentPoint <- commit:
            default:
                // Channel full - compaction will check alignment naturally
            }
        }
    }
}
```

### 4. DirCompactor Changes

**Key Question:** Which DirCompactors should compact at a given commit?

**Answer:** Only DirCompactors that have:
1. Processed segments up to that commit (`CurSegment.EndCommit >= commit`)
2. Accumulated enough segments (`Inputs >= Divisor`)
3. The commit is an alignment point (or pending intent indicates alignment)

**Design Decision:** Notify ALL DirCompactors at the level, but they only compact if conditions are met. This is simpler than tracking which paths have segments at which commits.

**New fields:**
```go
type DirCompactor struct {
    // ... existing fields ...
    alignmentPoint chan int64  // Receives alignment point commit numbers
    pendingAlignments []int64  // Sorted list of alignment points we've seen
    alignMu sync.Mutex
}
```

**Modified addSegment():**
```go
func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
    // ... existing logic to update CurSegment, increment Inputs ...
    dc.Inputs++
    
    // Check if we should compact NOW
    if dc.Inputs >= dc.Config.Divisor {
        return dc.shouldCompactNow(seg.EndCommit)
    }
    return false
}
```

**New shouldCompactNow():**
```go
func (dc *DirCompactor) shouldCompactNow(endCommit int64) bool {
    align := pow(dc.Config.Divisor, dc.Level)
    
    // Check if endCommit is an alignment point
    if endCommit % align == 0 {
        return true
    }
    
    // Check pending alignments
    dc.alignMu.Lock()
    defer dc.alignMu.Unlock()
    
    // Remove and check alignments <= endCommit
    // Only compact if we've actually processed segments up to the alignment point
    for i := len(dc.pendingAlignments) - 1; i >= 0; i-- {
        alignCommit := dc.pendingAlignments[i]
        if alignCommit <= endCommit {
            // We've processed segments up to endCommit, and alignCommit <= endCommit
            // So we've processed segments up to alignCommit
            // Remove this alignment (we'll compact)
            dc.pendingAlignments = append(dc.pendingAlignments[:i], dc.pendingAlignments[i+1:]...)
            return true
        }
    }
    
    return false
}
```

**Important:** The check `alignCommit <= endCommit` ensures we only compact when we've actually processed segments up to the alignment point. If a DirCompactor hasn't processed segments up to commit 2, `CurSegment.EndCommit` would be < 2, so `endCommit` (from the segment being processed) would also be < 2, and the check would fail.

**But wait:** What if a DirCompactor receives an intent for commit 2, but hasn't processed any segments yet? Then `CurSegment` is nil, and `endCommit` from the next segment might be > 2. We need to check `CurSegment.EndCommit` explicitly.

**Corrected shouldCompactNow():**
```go
func (dc *DirCompactor) shouldCompactNow(endCommit int64) bool {
    align := pow(dc.Config.Divisor, dc.Level)
    
    // Check if endCommit is an alignment point
    if endCommit % align == 0 {
        return true
    }
    
    // Check pending alignments
    dc.alignMu.Lock()
    defer dc.alignMu.Unlock()
    
    // Check if we have a pending alignment that we've reached
    for i := len(dc.pendingAlignments) - 1; i >= 0; i-- {
        alignCommit := dc.pendingAlignments[i]
        // Only compact if we've processed segments up to the alignment point
        if dc.CurSegment != nil && dc.CurSegment.EndCommit >= alignCommit {
            // We've processed segments up to alignCommit, compact now
            dc.pendingAlignments = append(dc.pendingAlignments[:i], dc.pendingAlignments[i+1:]...)
            return true
        }
        // If alignCommit > CurSegment.EndCommit, we haven't reached it yet
        // Keep the intent for later
    }
    
    return false
}
```

**Actually, simpler:** Check alignment when segment arrives, and check pending intents. If intent says "compact at commit 2" and we've processed segments up to commit 2, compact:

```go
func (dc *DirCompactor) shouldCompactNow(endCommit int64) bool {
    align := pow(dc.Config.Divisor, dc.Level)
    
    // Check if endCommit is an alignment point
    if endCommit % align == 0 {
        return true
    }
    
    // Check pending alignments - compact if we've reached an alignment point
    dc.alignMu.Lock()
    defer dc.alignMu.Unlock()
    
    // Find the highest alignment point we've reached
    if dc.CurSegment == nil {
        return false  // No segments processed yet
    }
    
    for i := len(dc.pendingAlignments) - 1; i >= 0; i-- {
        alignCommit := dc.pendingAlignments[i]
        if dc.CurSegment.EndCommit >= alignCommit {
            // We've processed segments up to alignCommit, compact now
            dc.pendingAlignments = append(dc.pendingAlignments[:i], dc.pendingAlignments[i+1:]...)
            return true
        }
    }
    
    return false
}
```

**Actually, simpler:** Track pending alignment points explicitly:

```go
type DirCompactor struct {
    // ... existing fields ...
    alignmentPoint chan int64  // Receives alignment point commit numbers
    pendingAlignments []int64  // Sorted list of alignment points we've seen
    alignMu sync.Mutex
}

func (dc *DirCompactor) shouldCompactNow(endCommit int64) bool {
    align := pow(dc.Config.Divisor, dc.Level)
    
    // Check if endCommit is an alignment point
    if endCommit % align == 0 {
        return true
    }
    
    // Check pending alignments
    dc.alignMu.Lock()
    defer dc.alignMu.Unlock()
    
    // Remove and check alignments <= endCommit
    for i := len(dc.pendingAlignments) - 1; i >= 0; i-- {
        if dc.pendingAlignments[i] <= endCommit {
            // Remove this alignment (we'll compact)
            dc.pendingAlignments = append(dc.pendingAlignments[:i], dc.pendingAlignments[i+1:]...)
            return true
        }
    }
    
    return false
}

func (dc *DirCompactor) run(env *storageEnv) {
    // ... recovery ...
    for {
        select {
        case seg, ok := <-dc.incoming:
            if !ok {
                return
            }
            if err := dc.processSegment(seg, env); err != nil {
                // ... recovery ...
            }
        case alignCommit := <-dc.alignmentPoint:
            // Store alignment point for later checking
            dc.alignMu.Lock()
            dc.pendingAlignments = insertSorted(dc.pendingAlignments, alignCommit)
            dc.alignMu.Unlock()
        case <-dc.done:
            return
        }
    }
}
```

### 5. Wire Up Callback

**Storage.Open():**
```go
// After creating compactor
s.compactor = compact.NewCompactor(compactorConfig, s.Seq, &s.indexMu, s.index)

// Set callback to write intents
s.Seq.SetOnCommitCallback(s.compactor.OnCommit)
```

## Lock Ordering Analysis

### Intent Creation
```
1. Seq.NextCommitLocked() called (seq lock held)
2. Commit incremented
3. OnCommit() called (seq lock still held)
4. Intent written/distributed (seq lock held - atomic!)
5. Seq lock released
```

**Safe:** Intent creation is atomic (seq lock held throughout).

### Intent Consumption
```
1. DirCompactor receives intent via channel (no locks)
2. Stores in pendingAlignments (alignMu held)
3. When processing segment, checks alignment (alignMu held)
```

**Safe:** No lock ordering issues. Intent creation and consumption are separate.

### Compaction Triggering
```
1. Segment processed (no locks in addSegment)
2. shouldCompactNow() checks alignment/intent (alignMu held)
3. If yes, rotateCompactionWindow() called
4. persistCurrent() acquires seq lock, then idxL lock
```

**Safe:** Same as current compaction path.

## Sequencing Guarantees

**Intent creation:** Atomic (seq lock held)
- Intents written in commit order
- No out-of-order issues
- No race conditions

**Intent consumption:** Separate goroutine
- Intents stored when received
- Checked when processing segments
- Segments processed in order (from channel)

**Compaction triggering:** When segment processed
- Segments processed in order (from channel)
- Alignment checked deterministically
- Intent ensures all paths see same alignment points

## Key Properties

1. **No Out-of-Order Issues:** Intents written atomically with seq lock
2. **Coordination:** All paths see same alignment points via intents
3. **Simple:** Alignment check is deterministic (`commit % align == 0`)
4. **Maintainable:** Clear separation between intent creation and consumption
5. **Robust:** No timing assumptions, no race conditions

## Edge Cases

1. **Intent arrives before segments:** Intent stored in `pendingAlignments`, checked when segments arrive. DirCompactor won't compact until it has processed segments up to the alignment point.

2. **DirCompactor has no segments at alignment point:** 
   - Example: `/a/b` has segments at commits 1, 3, 5, alignment point is commit 2
   - Intent arrives for commit 2, stored in `pendingAlignments`
   - When segment at commit 3 arrives, `CurSegment.EndCommit = 3`
   - Check: `CurSegment.EndCommit >= 2`? Yes, so compact
   - **Result:** Compacts at commit 3, not commit 2. This is acceptable - alignment is best-effort.

3. **Multiple intents:** Stored in sorted order, checked in order. Highest alignment point that's been reached triggers compaction.

4. **Intent for past commit:** If `CurSegment.EndCommit >= alignCommit`, compact (handles late arrivals or paths that process segments slowly).

5. **DirCompactor never reaches alignment point:**
   - Example: `/a/b` only has segments at commits 1, 3, 5, 7 (all odd)
   - Alignment points are 2, 4, 6, 8 (for divisor=2)
   - Intent arrives for commit 2, but `/a/b` never processes a segment at commit 2
   - When segment at commit 3 arrives, `CurSegment.EndCommit = 3 >= 2`, so compact
   - **Result:** Compacts at commit 3, which is after the alignment point. This is acceptable.

6. **Shutdown:** Intents in channel are lost (recovery handles state from disk, doesn't need intents).

## Which DirCompactors Compact?

**Answer:** All DirCompactors at the level are notified, but only those that have:
1. Processed segments up to the alignment point (`CurSegment.EndCommit >= alignCommit`)
2. Accumulated enough segments (`Inputs >= Divisor`)

**This means:**
- DirCompactors that haven't processed segments up to the alignment point won't compact (check fails)
- DirCompactors that don't have enough segments won't compact (check fails)
- DirCompactors that have both will compact

**No empty segments:** A DirCompactor only compacts when it has actual segments to compact. If it hasn't processed segments up to the alignment point, it simply doesn't compact (no empty segment created).

## Advantages Over Previous Approaches

1. **vs Commit-Driven (fragile):** No out-of-order callback issues (intents written atomically)
2. **vs Alignment-Only:** Coordinates all paths (intents ensure all see same alignment points)
3. **vs Complex Buffering:** Simple pending alignment list, checked when processing segments

## Open Questions (for implementation)

1. **Max level:** How many levels to check? (probably reasonable limit like 10)
2. **Intent cleanup:** Remove processed intents from `pendingAlignments`? (yes, when compacted)
3. **Recovery:** How to handle intents on restart? (recovery reads from disk, intents not needed)
4. **Channel size:** Buffer size for `alignmentPoint` channel? (small, like 10)
