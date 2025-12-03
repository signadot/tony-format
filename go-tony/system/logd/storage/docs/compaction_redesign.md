# Compaction Redesign: Commit-Driven Alignment

## Core Insight

**Current:** Compaction triggered by segment count (`Inputs >= Divisor`) → misaligned boundaries  
**Correct:** Compaction triggered by commit alignment (`N % (divisor^level) == 0`) → aligned boundaries

## Design

### Principle
Compaction happens at specific commits, not when segments accumulate. All paths compact together at alignment points.

### Flow

1. **Segments accumulate per-path** (as now, via `OnNewSegment`)
2. **When commit N is allocated:**
   - Check if `N % (divisor^level) == 0` for any level
   - If yes, trigger compaction check for all paths at that level
3. **Each path checks:** "Do I have >= divisor segments up to commit N?"
   - If yes → compact
   - If no → wait for next alignment point

### Changes

**Remove:**
- `Inputs >= Divisor` as compaction trigger
- Per-path independent compaction decisions

**Add:**
- `OnCommit(commit int64)` callback from sequencer to compactor
- Alignment check: `commit % (divisor^level) == 0`
- Per-level compaction pass at alignment points

**Keep:**
- Per-path segment accumulation (`Inputs` counter)
- `>= Divisor` check (but for "can I compact?", not "should I compact?")
- Per-path `DirCompactor` structure

## Implementation

### 1. Sequencer → Compactor callback
```go
// In seq/sequence.go - add callback support
type Seq struct {
    ...
    onCommit func(int64) // Called after commit is allocated
}

func (s *Seq) SetOnCommitCallback(fn func(int64)) {
    s.onCommit = fn
}
```

### 2. Compactor alignment check
```go
// In compact/compact.go
func (c *Compactor) OnCommit(commit int64) {
    // Check each level for alignment
    for level := 0; level < maxLevel; level++ {
        align := pow(c.Config.Divisor, level)
        if commit % align == 0 {
            c.triggerCompactionAtLevel(level, commit)
        }
    }
}

func (c *Compactor) triggerCompactionAtLevel(level int, commit int64) {
    // Check all paths at this level
    c.dcMu.Lock()
    defer c.dcMu.Unlock()
    for _, dc := range c.dcMap {
        if dc.Level == level && dc.hasEnoughSegments(commit) {
            dc.triggerCompaction(commit)
        }
    }
}
```

### 3. DirCompactor changes
```go
// In compact/dir_compact.go
// Remove: return dc.Inputs >= dc.Config.Divisor from addSegment()
// Add: explicit compaction trigger
func (dc *DirCompactor) triggerCompaction(commit int64) {
    if dc.Inputs >= dc.Config.Divisor && dc.CurSegment.EndCommit <= commit {
        // Compact now
    }
}
```

## Result

- **Simpler:** Single trigger mechanism (commit alignment)
- **Correct:** Boundaries naturally align (all paths see same commit)
- **Cleaner:** Removes per-path "when to compact" logic
- **Less code:** No coordination primitives needed
