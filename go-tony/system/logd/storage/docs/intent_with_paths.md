# Intent-Based Compaction: Path-Aware Design

## Core Idea

Instead of notifying ALL DirCompactors at a level, track which virtual paths actually have commits, and only notify those DirCompactors.

**Approach:**
1. **Intent Creation:** When commit N happens, paths are already in transaction log via `PendingFiles[].VirtualPath`
2. **Intent Structure:** On disk = `TxLogEntry` with `PendingFiles`, in memory = extract paths to `[]string` (no tree conversion needed)
3. **Alignment Check:** When any level compaction reaches alignment point, read last `divisor` commits from log
4. **Path Aggregation:** Extract unique paths from `PendingFiles`, then add their ancestors
5. **Selective Notification:** Notify DirCompactors for paths that had commits and their ancestors (optimizes read load)

## Design

### Path Representation

**On disk:** Paths are embedded in `TxLogEntry.PendingFiles[]` (each `FileRef` has `VirtualPath`)
```tony
{Commit: 2, PendingFiles: [{VirtualPath: "/a/b/c", TxSeq: 1}, {VirtualPath: "/a/d", TxSeq: 2}]}
```

**In memory:** List of paths (`[]string`) - extracted and deduplicated
- We extract paths from `TxLogEntry.PendingFiles[].VirtualPath` (deduplicated)
- Ancestor calculation is done on the path list, not a tree structure

### Intent Storage

**Transaction log:** Use existing `TxLogEntry` in `meta/transactions.log` (append-only)
- Format: One Tony object per line (codegen handles parsing via `FromTony()`)
- Structure: Existing `TxLogEntry`: `{Commit: 2, TxID: 2, Timestamp: "...", PendingFiles: [{VirtualPath: "/a/b/c", TxSeq: 1}, {VirtualPath: "/a/d", TxSeq: 2}]}`
- Paths are extracted from `PendingFiles[].VirtualPath` (no new field needed)
- Written as part of commit operation (same transaction, same log entry)
- No fsync (matches existing transaction log behavior)
- **No codegen changes:** Uses existing `TxLogEntry` structure as-is
- **Synchronization:** Uses existing `logMu` RWMutex

**Why transaction log:**
- Natural ordering: commits appear in order with their paths (via `PendingFiles`)
- Single source of truth: paths are in `PendingFiles`, no duplication
- Simple: append-only log, easy to read
- Atomic: written as part of commit (no separate write)

**Why extract from `PendingFiles`:**
- No data duplication: paths already exist in `PendingFiles[].VirtualPath`
- No consistency issues: paths always match actual files
- Simpler: no new field, no extra population logic
- Correct: paths are guaranteed to match files that were committed

**Failed/Aborted Commits:**
- **Aborted before commit:** No files written, no log entry → Not in log, nothing to compact ✓
- **Aborted during commit:** Files may be written but transaction aborts → Files deleted, no log entry → Not in log, nothing to compact ✓
- **Aborted after commit, before log write:** Files committed (.diff files exist), but log write fails → Transaction aborts, but `deletePathAt` only deletes .pending files, not .diff files
  - **Edge case:** Files exist on disk but no log entry
  - **Compaction behavior:** Only reads from log, so won't see these commits (correct - they're not fully committed)
  - **System consistency:** These orphaned files should be handled by recovery/reconciliation, not compaction
- **Successfully committed:** Files committed AND log entry written → In log, compaction sees it ✓

**Design assumption:** Compaction only processes commits that are fully committed (log entry exists). This ensures compaction only works with consistent, recoverable commits. Orphaned files (exist but no log entry) are a system inconsistency that should be handled separately by recovery mechanisms.

### Intent Creation

**Location:** `tx.go` transaction commit (paths already available in `PendingFiles`)

**Structure:** Move `TxLogEntry` and `FileRef` to shared `types` package
```go
// Package: types
// TxLogEntry represents a transaction commit log entry.
//
//tony:schemagen=txlog-entry
type TxLogEntry struct {
    Commit       int64
    TxID         int64
    Timestamp    string // RFC3339 timestamp
    PendingFiles []FileRef  // Already contains VirtualPath for each file
}

// FileRef already has VirtualPath
//
//tony:schemagen=pending-file-ref
type FileRef struct {
    VirtualPath string
    TxSeq       int64
}
```

**Package structure:**
- `storage/types/` - New package for shared types
  - `types.go` - Contains `TxLogEntry` and `FileRef`
  - `types_gen.go` - Codegen'd methods (generated)

**Writing:** Use existing `AppendTxLog` (update import to use `types` package)
- Paths are already in `PendingFiles[].VirtualPath`
- No new field needed - extract paths when reading
- `AppendTxLog` now uses `types.TxLogEntry` instead of `TxLogEntry`
- Existing encoding and file writing logic unchanged

**Note:** The transaction log entry is created in `tx.go:337` with `PendingFiles` already populated. Paths are extracted later during compaction reading, not during writing. After moving types to `types` package, update imports in `log.go` and `tx.go`.

### Intent Reading

**When:** Any level compaction reaches alignment point (when `Inputs >= Divisor` and `EndCommit % align == 0`)

**What:** Read last `divisor` commits from transaction log

**Reading:** Read from `meta/transactions.log` (in `compact` package)

**Solution: Shared types package**
- Create `types` package: `github.com/signadot/tony-format/go-tony/system/logd/storage/types`
- Move `TxLogEntry` and `FileRef` from `storage` package to `types` package
- Both `storage` and `compact` can import `types` (no circular dependency)
- Codegen annotations (`//tony:schemagen`) move with the types

**Implementation:**
```go
// Package: types
package types

// TxLogEntry represents a transaction commit log entry.
//
//tony:schemagen=txlog-entry
type TxLogEntry struct {
    Commit       int64
    TxID         int64
    Timestamp    string // RFC3339 timestamp
    PendingFiles []FileRef
}

// FileRef references a file in a transaction log entry.
//
//tony:schemagen=pending-file-ref
type FileRef struct {
    VirtualPath string
    TxSeq       int64 // Transaction sequence number
}
```

**Usage in `compact` package:**
```go
// Package: compact
import "github.com/signadot/tony-format/go-tony/system/logd/storage/types"

// CompactionIntent is a simple in-memory structure
type CompactionIntent struct {
    Commit int64
    Paths  []string  // Direct list of paths, no tree conversion
}

func (c *Compactor) readCommits(startCommit, endCommit int64) ([]CompactionIntent, error) {
    logFile := filepath.Join(c.Config.Root, "meta", "transactions.log")
    
    // Read entire file (simple approach, can optimize later with binary search)
    data, err := os.ReadFile(logFile)
    if err != nil {
        if os.IsNotExist(err) {
            return []CompactionIntent{}, nil  // File doesn't exist yet
        }
        return nil, err
    }
    
    return c.parseTxLogLines(data, startCommit, endCommit)
}

func (c *Compactor) parseTxLogLines(data []byte, startCommit, endCommit int64) ([]CompactionIntent, error) {
    intents := []CompactionIntent{}
    lines := strings.Split(string(data), "\n")
    
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        
        // Parse using codegen FromTony() method (now from types package)
        entry := &types.TxLogEntry{}
        if err := entry.FromTony([]byte(line)); err != nil {
            c.Config.Log.Warn("skipping invalid transaction log entry", "error", err)
            continue  // Skip invalid entries
        }
        
        if entry.Commit >= startCommit && entry.Commit <= endCommit {
            // Extract unique paths from PendingFiles
            paths := extractPathsFromPendingFiles(entry.PendingFiles)
            intents = append(intents, CompactionIntent{
                Commit: entry.Commit,
                Paths:  paths,  // Extract from PendingFiles
            })
        }
    }
    
    return intents, nil
}

// extractPathsFromPendingFiles extracts unique virtual paths from PendingFiles
func extractPathsFromPendingFiles(pendingFiles []types.FileRef) []string {
    pathSet := make(map[string]bool)
    for _, ref := range pendingFiles {
        pathSet[ref.VirtualPath] = true
    }
    
    // Convert set to slice
    paths := make([]string, 0, len(pathSet))
    for path := range pathSet {
        paths = append(paths, path)
    }
    // Note: Could sort paths here if deterministic order is needed
    return paths
}
```

**Benefits:**
- No circular dependency: `storage` → `compact`, both → `types`
- Reuses codegen: `TxLogEntry` and `FileRef` keep their `ToTony()`/`FromTony()` methods
- Clean separation: shared types in dedicated package
- Type safety: Full type checking, no manual parsing needed

### Path Ancestor Calculation

**Strategy:** Notify paths that had commits plus all their ancestors.

**Rationale:**
- When reading `/a/b/c`, `LookupRange("/a/b/c", ...)` includes segments from ancestors `/a` and `/a/b`
- If ancestors are compacted, descendant reads are faster
- Compacting descendants doesn't help ancestor reads (they're separate paths)
- **Rule:** Compact paths that had commits and their ancestors

**Ancestor calculation:**
```go
func addAncestors(pathSet map[string]bool, path string) {
    if path == "" || path == "/" {
        return  // Root has no ancestors
    }
    parts := strings.Split(strings.Trim(path, "/"), "/")
    for i := 1; i < len(parts); i++ {
        ancestor := "/" + strings.Join(parts[:i], "/")
        pathSet[ancestor] = true
    }
    pathSet["/"] = true  // Root is ancestor of everything
}
```

**Example:**
- Intent paths: `["/a/b/c"]`
- After adding ancestors: `/a/b/c`, `/a/b`, `/a`, `/`
- When reading `/a/b/c`, all ancestors are compacted → fast read

### Notification Logic

**When:** Called by `DirCompactor` when it reaches an alignment point (when `Inputs >= Divisor` and `EndCommit % align == 0`)

**What:** Read commits from transaction log, extract paths + ancestors, notify next-level DirCompactors

**Implementation:** (in `compact` package)
```go
// OnAlignmentPointReached is called by DirCompactor when it reaches an alignment point.
// It reads commit intents from the transaction log and notifies relevant DirCompactors at the next level.
func (c *Compactor) OnAlignmentPointReached(level int, alignCommit int64) {
    // Read commits for alignment window
    startCommit := alignCommit - int64(c.Config.Divisor) + 1
    intents, err := c.readCommits(startCommit, alignCommit)
    if err != nil {
        c.Config.Log.Warn("failed to read commit log", "error", err)
        return  // Skip notification if log read fails
    }
    
    // Extract all paths from intents and add their ancestors
    pathSet := make(map[string]bool)
    for _, intent := range intents {
        for _, path := range intent.Paths {
            pathSet[path] = true
            // Add all ancestors (they help reads of this path)
            addAncestors(pathSet, path)
        }
    }
    
    // Notify next level DirCompactors for paths + ancestors
    nextLevel := level + 1
    c.dcMu.Lock()
    defer c.dcMu.Unlock()
    
    // For each path (including ancestors), look up DirCompactor and notify
    for path := range pathSet {
        // Get level 0 DirCompactor for this path (getOrInitDC always returns level 0)
        dc := c.getOrInitDC(&index.LogSegment{RelPath: path})
        if dc == nil {
            continue
        }
        
        // Traverse to next level in chain
        // Note: We always start from level 0 (base DirCompactor), then traverse 'nextLevel' steps
        // to reach level 'nextLevel'. This works because:
        // - If caller is level 0, nextLevel=1: traverse 1 step (0→1) ✓
        // - If caller is level 1, nextLevel=2: traverse 2 steps (0→1→2) ✓
        // The chain is: level 0 → level 1 → level 2 → ...
        for i := 0; i < nextLevel; i++ {
            if dc.Next == nil {
                // Create next level: current level + 1 (matches existing pattern in rotateCompactionWindow)
                dc.Next = NewDirCompactor(&c.Config, dc.Level+1, dc.Dir, dc.VirtualPath, c.env)
            }
            dc = dc.Next
        }
        
        // Notify alignment point reached
        select {
        case dc.alignmentPoint <- alignCommit:
        default:
        }
    }
}
```

**Filtering:** 
- **Reading:** Filter commits by range (last `divisor` commits)
- **Notification:** Notify paths that had commits plus all their ancestors
- **Compaction check:** DirCompactor checks `Inputs >= Divisor` AND `CurSegment.EndCommit >= alignCommit` (has processed segments up to alignment point)

## Compaction Strategy: Design for Useful Properties

**Goal:** Choose a strategy with properties that make the system useful and interesting, not optimize for unknown read patterns.

**Key property:** `LookupRange` returns ancestors, target, and descendants. So reads of `/a/b/c` include segments from `/a` (ancestor) and `/a/b` (ancestor).

**Strategy: Notify paths + ancestors**

**Rationale:**
1. **Ancestors help descendant reads:** When reading `/a/b/c`, `LookupRange` includes `/a` and `/a/b` segments. If ancestors are compacted, descendant reads are faster.
2. **Descendants don't help ancestor reads:** When reading `/a/b`, `LookupRange` includes `/a/b/c` segments, but compacting `/a/b/c` doesn't help `/a/b` reads (they're separate paths).
3. **Predictable:** Clear rule - compact paths that had commits and their ancestors.
4. **Efficient:** Don't waste work compacting descendants that don't help ancestor reads.

**Properties:**
- **Read performance:** Reads of any path benefit from compacted ancestors
- **Storage efficiency:** Compact at alignment points, reducing segment count
- **Predictability:** Clear, simple rule - easy to reason about
- **Scalability:** Works well as hierarchy grows

**Example:**
- Intent paths: `["/a/b/c"]`
- After adding ancestors: `/a/b/c`, `/a/b`, `/a`, `/`
- When reading `/a/b/c`, all ancestors are compacted → fast read
- When reading `/a/b`, ancestor `/a` is compacted → fast read
- When reading `/a`, it's compacted → fast read

## How Readers Get Lower-Level Changes

**Question:** If we compact ancestors (like `/a`), how do readers of descendants (like `/a/b/c`) get those changes?

**Answer:** `LookupRange` handles this automatically.

**How it works:**
1. Reader calls `ReadStateAt("/a/b/c", commit)`
2. `ReadStateAt` calls `s.index.LookupRange("/a/b/c", from, to)`
3. `LookupRange` recursively traverses the index tree:
   - Gets segments from root (ancestor of `/a`)
   - Gets segments from `/a` index (ancestor of `/a/b`)
   - Gets segments from `/a/b` index (ancestor of `/a/b/c`)
   - Gets segments from `/a/b/c` index (target)
   - Gets segments from descendants of `/a/b/c`
4. Returns all segments (ancestors + target + descendants)
5. Reader applies segments in order to reconstruct state

**Key point:** If `/a` is compacted, `LookupRange` returns the compacted `/a` segment. The reader doesn't need to do anything special - `LookupRange` automatically includes ancestor segments.

**Example:**
- `/a` compacts at commit 4 (level 1 segment covering commits 1-4)
- Reader queries `/a/b/c @5`
- `LookupRange("/a/b/c", nil, 5)` returns:
  - Compacted `/a` segment (commits 1-4) ← from ancestor
  - `/a/b` segments (if any)
  - `/a/b/c` segments (commits 5+)
- Reader applies: compacted `/a` segment + `/a/b/c` segments = correct state

**No special handling needed:** `LookupRange` via the index automatically provides ancestor segments, including compacted ones.

## Advantages

1. **Precise:** Only notify DirCompactors for paths that had commits (plus ancestors for read optimization)
2. **No empty segments:** DirCompactors without segments in window don't compact
3. **Persistent:** Intents survive restarts (in transaction log)
4. **Simple representation:** List of paths on disk and in memory (no tree conversion needed)
5. **Efficient:** Direct path operations, ancestor calculation is straightforward

## Implementation Notes

1. **Codegen:** Move `TxLogEntry` and `FileRef` to `types` package
   - Create new `storage/types/` package
   - Move types from `log.go` to `types/types.go`
   - Codegen annotations (`//tony:schemagen`) move with types
   - Codegen generates `types/types_gen.go` with `ToTony()`/`FromTony()` methods
   - Both `storage` and `compact` import `types` package
   - File: `meta/transactions.log` (existing file, no changes needed)
   - Paths are extracted from `PendingFiles[].VirtualPath` during reading

2. **Path extraction:** Paths come from `PendingFiles` in `TxLogEntry`
   - In `tx.go`, the transaction already tracks `PendingFiles []FileRef` (which has `VirtualPath`)
   - Each `FileRef` has a `VirtualPath` field
   - Extract unique virtual paths from `PendingFiles` when reading transaction log
   - Extraction happens in `parseTxLogLines()` via `extractPathsFromPendingFiles()`
   - No changes needed to transaction commit code (`tx.go:337`)

3. **Reading efficiency:** How to efficiently read last N commits?
   - **Option A:** Scan entire file, filter by commit range (simple but slow for large files)
   - **Option B:** Scan from end backwards (if we only need recent commits)
   - **Option C:** Keep index file (commit -> file offset)
   - **Option D:** Rotate log files periodically
   - **Recommendation:** Start with Option A, optimize to Option B if needed
   - Note: Alignment windows are small (`divisor` commits), so scanning is acceptable

4. **Alignment point triggering:** When is `OnAlignmentPointReached` called?
   - Called by `DirCompactor` when it detects an alignment point
   - Detection: `Inputs >= Divisor` AND `CurSegment.EndCommit % align == 0`
   - The `DirCompactor` calls `c.OnAlignmentPointReached(level, alignCommit)` before compacting

5. **Log cleanup:** When to remove old log entries?
   - Keep all entries (they're small, append-only)
   - Or: Rotate/truncate after some window

6. **Failed/aborted commits:** How does compaction handle them?
   - Compaction only reads from transaction log (`meta/transactions.log`)
   - Only fully committed transactions (with log entries) are processed
   - Aborted transactions don't write to log → Not seen by compaction (correct)
   - Edge case: If log write fails after files are committed, files may exist but no log entry
     - Compaction won't see these (correct - they're not fully committed)
     - These orphaned files should be handled by recovery/reconciliation, not compaction
   - **Design principle:** Compaction operates on the transaction log as the source of truth for committed transactions
