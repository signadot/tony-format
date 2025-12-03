# Compaction Alignment: Design Alternatives

## Problem with Current Proposal

The commit-driven approach has a fundamental issue:
- `OnCommit()` calls can arrive out of order (after `indexMu` is released)
- This creates subtle timing dependencies that are hard to reason about
- The explanation relies on "segments arrive before OnCommit()" which is a timing assumption, not a guarantee

## Alternative 1: Check Alignment When Processing Segments

**Approach:** When `Inputs >= Divisor`, check if `CurSegment.EndCommit` is an alignment point.

```go
func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
    // ... existing logic ...
    dc.Inputs++
    
    // Check if we should compact NOW (not just "can we compact?")
    if dc.Inputs >= dc.Config.Divisor {
        align := pow(dc.Config.Divisor, dc.Level)
        if dc.CurSegment.EndCommit % align == 0 {
            return true  // Trigger compaction
        }
        // Not at alignment point yet - keep accumulating
        return false
    }
    return false
}
```

**Pros:**
- Simple - no separate trigger mechanism
- No out-of-order issues - segments processed in order
- Natural alignment check

**Cons:**
- **Problem:** Segments might accumulate past alignment point
  - Example: Segments at commits 1, 2, 3 arrive
  - At commit 2, we have 2 segments, but commit 2 is alignment point
  - But we only check when segment arrives, so we might miss it
  - Or we check after each segment, but then we compact at commit 2, not commit 3

**Actually, this might work:** Check alignment after adding each segment. If we have enough segments AND we're at an alignment point, compact.

## Alternative 2: Buffer Until Alignment Point

**Approach:** When `Inputs >= Divisor`, don't compact immediately. Instead, wait until `CurSegment.EndCommit` is an alignment point.

```go
func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
    // ... existing logic ...
    dc.Inputs++
    
    if dc.Inputs >= dc.Config.Divisor {
        align := pow(dc.Config.Divisor, dc.Level)
        // Check if current segment's EndCommit is an alignment point
        if dc.CurSegment.EndCommit % align == 0 {
            return true  // Trigger compaction NOW
        }
        // Not at alignment point - keep accumulating (buffer)
        return false
    }
    return false
}
```

**Pros:**
- Simple - no separate trigger
- Segments processed in order
- Natural buffering until alignment point

**Cons:**
- **Problem:** What if segments arrive at non-alignment commits?
  - Segments at commits 1, 3, 5, 7... (all odd)
  - We accumulate 2 segments (commits 1, 3)
  - Commit 3 is not an alignment point (for divisor=2, alignment is 2, 4, 6...)
  - We keep accumulating: commits 1, 3, 5 (3 segments)
  - Still not at alignment point
  - Eventually we have many segments but never hit alignment point
  - **Solution:** Compact at next alignment point >= EndCommit, not exactly at EndCommit

**Better version:**
```go
func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
    // ... existing logic ...
    dc.Inputs++
    
    if dc.Inputs >= dc.Config.Divisor {
        align := pow(dc.Config.Divisor, dc.Level)
        // Check if we've reached or passed an alignment point
        nextAlign := ((dc.CurSegment.EndCommit / align) + 1) * align
        if dc.CurSegment.EndCommit >= nextAlign - align {
            // We've accumulated enough segments and we're at/past an alignment point
            return true
        }
        return false
    }
    return false
}
```

Wait, that's still wrong. Let me think...

Actually, the issue is: we want to compact at commit N where N % align == 0. But segments arrive with EndCommit values. We need to wait until we have a segment with EndCommit that's an alignment point.

**Correct version:**
```go
func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
    // ... existing logic ...
    dc.Inputs++
    
    if dc.Inputs >= dc.Config.Divisor {
        align := pow(dc.Config.Divisor, dc.Level)
        // Check if this segment's EndCommit is an alignment point
        if seg.EndCommit % align == 0 {
            return true  // Trigger compaction at this alignment point
        }
        // Not at alignment point - keep accumulating
        return false
    }
    return false
}
```

**But this has the same problem:** What if segments arrive at commits 1, 3, 5? We never hit an alignment point.

**Solution:** Don't require exact alignment. Instead, compact when we have enough segments AND we're at/past an alignment point:

```go
func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
    // ... existing logic ...
    dc.Inputs++
    
    if dc.Inputs >= dc.Config.Divisor {
        align := pow(dc.Config.Divisor, dc.Level)
        // Find the last alignment point <= EndCommit
        lastAlign := (seg.EndCommit / align) * align
        // If we've accumulated enough segments and we're at/past an alignment point
        if lastAlign > 0 && seg.EndCommit >= lastAlign {
            return true
        }
        return false
    }
    return false
}
```

Actually, I think the real issue is: we want ALL paths to compact at the SAME alignment points. But if paths receive segments at different times, they'll hit alignment points at different times.

## Alternative 3: Explicit Alignment Tracking

**Approach:** Track "next alignment point" and only compact when we reach it.

```go
type DirCompactor struct {
    // ... existing fields ...
    nextAlignmentPoint int64  // Next commit where we should compact
}

func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
    // ... existing logic ...
    dc.Inputs++
    
    if dc.Inputs >= dc.Config.Divisor {
        // Check if we've reached the next alignment point
        if seg.EndCommit >= dc.nextAlignmentPoint {
            // Update next alignment point for next compaction
            align := pow(dc.Config.Divisor, dc.Level)
            dc.nextAlignmentPoint = ((seg.EndCommit / align) + 1) * align
            return true
        }
        return false
    }
    return false
}
```

**Pros:**
- Explicit tracking of alignment points
- Segments processed in order
- No out-of-order issues

**Cons:**
- Still doesn't guarantee all paths compact at same time
- Paths receiving segments at different times will compact at different times

## The Real Problem

The fundamental issue is: **we want all paths to compact together at alignment points, but segments arrive asynchronously per path.**

This is inherently difficult with per-path compaction. The only way to guarantee alignment is:
1. **Synchronous coordination** - all paths wait for alignment point (complex, blocking)
2. **Accept misalignment** - paths compact independently, alignment is best-effort
3. **Global compaction** - single compaction stream for all paths (major redesign)

## Recommendation

Given the complexity and fragility of commit-driven compaction with out-of-order callbacks, I recommend:

**Option A:** Accept that alignment is best-effort. Keep current count-driven compaction, but add alignment check:
- When `Inputs >= Divisor`, check if `CurSegment.EndCommit` is an alignment point
- If yes, compact. If no, compact anyway (current behavior)
- This gives alignment when possible, but doesn't guarantee it

**Option B:** Revisit the requirement. Is perfect alignment necessary? Or is "compaction happens at alignment points when possible" sufficient?

**Option C:** If perfect alignment is required, consider global compaction (single stream) rather than per-path compaction.
