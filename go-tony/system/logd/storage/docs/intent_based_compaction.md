# Intent-Based Compaction: Reference Design

## Overview

Intent-based compaction coordinates compaction across the virtual path hierarchy by tracking which paths have commits and notifying only relevant `DirCompactor` instances at alignment points. This ensures compaction happens at coordinated boundaries while avoiding unnecessary work on paths without commits.

## Correctness Property

**Core correctness:** Read results for any compaction state are coherent w.r.t. compaction windows. Querying `/a/b @N` at a compaction boundary `N` (where `N % (divisor^level) == 0`) yields a precise view of `/a/b` at that commit.

## Architecture

### Path Tracking

Paths are extracted from the transaction log (`meta/transactions.log`) which already contains `TxLogEntry.PendingFiles[]`. Each `FileRef` has a `VirtualPath` field, so no new fields are needed.

**On disk:** Paths embedded in `TxLogEntry.PendingFiles[]`
```tony
{Commit: 2, PendingFiles: [{VirtualPath: "/a/b/c", TxSeq: 1}, {VirtualPath: "/a/d", TxSeq: 2}]}
```

**In memory:** List of paths (`[]string`) - extracted and deduplicated from `PendingFiles[].VirtualPath`

### Types Package

Shared types are in `storage/types/` package to avoid circular dependencies (`storage` imports `compact`, both import `types`).

```go
// Package: types
//tony:schemagen=txlog-entry
type TxLogEntry struct {
    Commit       int64
    TxID         int64
    Timestamp    string
    PendingFiles []FileRef
}

//tony:schemagen=pending-file-ref
type FileRef struct {
    VirtualPath string
    TxSeq       int64
}
```

### Intent Reading

When a `DirCompactor` reaches an alignment point (`Inputs >= Divisor` AND `EndCommit % align == 0`), it calls `Compactor.OnAlignmentPointReached()` which:

1. Reads last `divisor` commits from `meta/transactions.log`
2. Extracts unique paths from `PendingFiles[].VirtualPath`
3. Adds all ancestor paths (for read optimization)
4. Notifies `DirCompactor` instances at the next level for those paths

**Reading implementation:**
```go
func (c *Compactor) readCommits(startCommit, endCommit int64) ([]CompactionIntent, error)
func (c *Compactor) parseTxLogLines(data []byte, startCommit, endCommit int64) ([]CompactionIntent, error)
func extractPathsFromPendingFiles(pendingFiles []types.FileRef) []string
```

**CompactionIntent structure:**
```go
type CompactionIntent struct {
    Commit int64
    Paths  []string  // Deduplicated paths from PendingFiles
}
```

### Path Ancestor Calculation

**Strategy:** Notify paths that had commits plus all their ancestors.

**Rationale:**
- When reading `/a/b/c`, `LookupRange("/a/b/c", ...)` includes segments from ancestors `/a` and `/a/b`
- If ancestors are compacted, descendant reads are faster
- Compacting descendants doesn't help ancestor reads (they're separate paths)

**Implementation:**
```go
func addAncestors(pathSet map[string]bool, path string) {
    if path == "" || path == "/" {
        return
    }
    parts := strings.Split(strings.Trim(path, "/"), "/")
    for i := 1; i < len(parts); i++ {
        ancestor := "/" + strings.Join(parts[:i], "/")
        pathSet[ancestor] = true
    }
    pathSet["/"] = true
}
```

**Example:** Commit to `/a/b/c` notifies `/a/b/c`, `/a/b`, `/a`, `/`

### Alignment Point Detection

**When:** `DirCompactor` detects alignment point during segment processing:
- `Inputs >= Divisor` (enough segments accumulated)
- `CurSegment.EndCommit % align == 0` (where `align = divisor^(level+1)`)

**Alignment calculation:**
```go
func alignmentForLevel(divisor int, level int) int64 {
    align := int64(1)
    for i := 0; i <= level; i++ {
        align *= int64(divisor)
    }
    return align
}
```

**Detection:** After processing a segment, `DirCompactor` checks:
```go
align := alignmentForLevel(c.Config.Divisor, dc.Level)
if dc.Inputs >= c.Config.Divisor && dc.CurSegment.EndCommit % align == 0 {
    c.OnAlignmentPointReached(dc.Level, dc.CurSegment.EndCommit)
}
```

### Notification Logic

**OnAlignmentPointReached implementation:**
```go
func (c *Compactor) OnAlignmentPointReached(level int, alignCommit int64) {
    // Read commits for alignment window
    startCommit := alignCommit - int64(c.Config.Divisor) + 1
    intents, err := c.readCommits(startCommit, alignCommit)
    if err != nil {
        c.Config.Log.Warn("failed to read commit log", "error", err)
        return
    }
    
    // Extract all paths from intents and add their ancestors
    pathSet := make(map[string]bool)
    for _, intent := range intents {
        for _, path := range intent.Paths {
            pathSet[path] = true
            addAncestors(pathSet, path)
        }
    }
    
    // Notify next level DirCompactors for paths + ancestors
    nextLevel := level + 1
    c.dcMu.Lock()
    defer c.dcMu.Unlock()
    
    for path := range pathSet {
        dc := c.getOrInitDC(&index.LogSegment{RelPath: path})
        if dc == nil {
            continue
        }
        
        // Traverse to next level in chain
        for i := 0; i < nextLevel; i++ {
            if dc.Next == nil {
                dc.Next = NewDirCompactor(&c.Config, dc.Level+1, dc.Dir, dc.VirtualPath, c.env)
            }
            dc = dc.Next
        }
        
        // Notify alignment point reached (non-blocking)
        select {
        case dc.alignmentPoint <- alignCommit:
        default:
        }
    }
}
```

### DirCompactor Notification Handling

**Alignment notification channel:**
```go
type DirCompactor struct {
    // ... existing fields
    alignmentPoint chan int64  // Alignment commit notifications
    pendingAlignments []int64  // Alignment commits waiting to be processed
}
```

**Notification handling in run():**
```go
select {
case seg, ok := <-dc.incoming:
    // ... process segment
case alignCommit := <-dc.alignmentPoint:
    dc.pendingAlignments = append(dc.pendingAlignments, alignCommit)
case <-dc.done:
    return nil
}
```

**Compaction decision:**
```go
func (dc *DirCompactor) shouldCompactNow() bool {
    if dc.Inputs < dc.Config.Divisor {
        return false
    }
    
    // Check if we've reached an alignment point
    align := alignmentForLevel(dc.Config.Divisor, dc.Level)
    if dc.CurSegment.EndCommit % align == 0 {
        return true
    }
    
    // Check if we've processed segments up to a pending alignment
    for _, alignCommit := range dc.pendingAlignments {
        if dc.CurSegment.EndCommit >= alignCommit {
            return true
        }
    }
    
    return false
}
```

## Key Components

### Compactor Methods

- `OnAlignmentPointReached(level int, alignCommit int64)` - Called by `DirCompactor` at alignment points
- `readCommits(startCommit, endCommit int64) ([]CompactionIntent, error)` - Reads transaction log
- `parseTxLogLines(data []byte, startCommit, endCommit int64) ([]CompactionIntent, error)` - Parses log entries

### DirCompactor Changes

- `alignmentPoint chan int64` - Receives alignment notifications
- `pendingAlignments []int64` - Tracks pending alignments
- Alignment detection in `processSegment()` or `shouldCompactNow()`
- Notification handling in `run()` select statement

### Helper Functions

- `extractPathsFromPendingFiles(pendingFiles []types.FileRef) []string` - Extracts unique paths
- `addAncestors(pathSet map[string]bool, path string)` - Adds ancestor paths
- `alignmentForLevel(divisor int, level int) int64` - Calculates alignment for level

## Correctness Guarantees

1. **Coherent Read Results:** Querying `/a/b @N` at alignment point `N` returns correct state
2. **Path Coordination:** Only paths with commits (+ ancestors) are notified
3. **No False Positives:** DirCompactors without commits are not notified
4. **No False Negatives:** All relevant DirCompactors (paths + ancestors) are notified
5. **Alignment Coordination:** Compaction coordinates across hierarchy at alignment points
6. **Path Matching:** Paths extracted match actual files (from `PendingFiles`)

## Failed/Aborted Commits

- **Aborted transactions:** Don't write to log → Not seen by compaction (correct)
- **Edge case:** If log write fails after files committed, files may exist but no log entry
  - Compaction won't see these (correct - they're not fully committed)
  - Orphaned files handled by recovery/reconciliation, not compaction

**Design assumption:** Compaction only processes commits that are fully committed (log entry exists).

## Dependencies

- **Types package:** `storage/types/` (shared by `storage` and `compact`)
- **Transaction log:** `meta/transactions.log` (existing file)
- **Codegen:** `TxLogEntry` and `FileRef` use `//tony:schemagen` annotations

## Implementation Reference

See `docs/implementation_plan_intent_based.md` for detailed implementation steps.

See `docs/intent_with_paths.md` for the complete design specification with rationale.
