# Index Critique: Is On-Disk Index Really Needed?

## Current Design

- **Logs**: Sequential append-only files (`level0.log`, `level1.log`, etc.)
- **Index**: Hierarchical in-memory structure mapping `(path, level)` → `(logPosition, commitRange)`
- **Recovery**: Index rebuilt from logs on startup

## What Does the Index Actually Do?

1. **Fast Path Lookups**: Given path `/a/b` and commit N, quickly find log positions
2. **Range Queries**: Find all entries for a path in commit range [from, to]
3. **Hierarchy Navigation**: Find child paths under a parent
4. **Compaction Alignment**: Find entries at alignment points for compaction

## Key Insight: Index is NOT the Source of Truth

The **logs are the source of truth**. The index is just a **cache/optimization** to avoid scanning logs.

## Critique: Do We Need to Persist the Index?

### Option A: In-Memory Index Only (Rebuild on Startup)

**How it works:**
- Index lives only in memory
- On startup: Scan all log files, parse entries, rebuild index
- During operation: Update in-memory index as entries are written
- On crash: Index lost, but logs intact → rebuild on restart

**Pros:**
- ✅ Simpler: No index persistence logic
- ✅ No index corruption concerns
- ✅ Index always matches logs (rebuild ensures consistency)
- ✅ One less thing to maintain
- ✅ Logs are the single source of truth

**Cons:**
- ❌ Startup time: Must scan all logs to rebuild
- ❌ Memory: Index must fit in memory (but it's just pointers/metadata, not data)

**Startup Cost Analysis:**
- For 1M log entries: ~1-10 seconds to scan and parse (depends on log size)
- For 10M log entries: ~10-100 seconds
- **Question**: Is this acceptable? Probably yes for most use cases

### Option B: Persisted Index (Current Approach)

**How it works:**
- Index persisted to disk (somehow - not clear how in current design)
- On startup: Load index from disk
- During operation: Update both in-memory and on-disk index
- On crash: Index might be stale, need to rebuild/verify

**Pros:**
- ✅ Fast startup (load index, don't scan logs)
- ✅ Can verify index against logs (incremental check)

**Cons:**
- ❌ Complexity: Need to persist index somehow
- ❌ Consistency: Index might be out of sync with logs
- ❌ Corruption: Index file might be corrupted
- ❌ Maintenance: Two sources of truth (logs + index)

### Option C: No Index At All (Scan Logs Every Time)

**How it works:**
- No index, just scan logs for every read
- Parse entries, filter by path and commit

**Pros:**
- ✅ Simplest possible design
- ✅ No consistency concerns
- ✅ No memory overhead

**Cons:**
- ❌ **Very slow**: Must scan entire log for every read
- ❌ **Unacceptable performance**: For 1M entries, every read scans 1M entries
- ❌ **Not practical**: This is a non-starter

**Verdict**: ❌ Not viable for any real workload

## Analysis: Is In-Memory Index Sufficient?

### What Does the Index Actually Store?

The index stores:
- Path strings (small)
- Log positions (int64, 8 bytes)
- Commit ranges (int64, 16 bytes per segment)
- Tree structure (pointers, small)

**Memory Estimate:**
- Per entry: ~100-200 bytes (path string + metadata)
- 1M entries: ~100-200 MB
- 10M entries: ~1-2 GB

**Verdict**: ✅ Reasonable memory footprint

### Startup Performance

**Scanning logs to rebuild index:**
- Sequential read of log files (fast, OS cache helps)
- Parse Tony wire format entries (efficient)
- Build index structure (in-memory, fast)

**Estimated time:**
- 1M entries: 1-5 seconds
- 10M entries: 10-50 seconds
- 100M entries: 100-500 seconds (might need optimization)

**Verdict**: ✅ Acceptable for most use cases, might need optimization for very large systems

### What About Compaction?

Compaction needs to:
- Find entries at alignment points
- Query entries in commit ranges
- Navigate hierarchy

**Without index**: Would need to scan logs (slow)
**With in-memory index**: Fast lookups

**Verdict**: ✅ In-memory index is sufficient for compaction

## The Real Question: Why Persist the Index?

### Arguments FOR Persisting:

1. **Fast Startup**: Don't want to wait 10 seconds to start
   - **Counter**: Is 10 seconds really a problem? Most systems can wait
   - **Counter**: Can optimize startup (parallel scanning, incremental rebuild)

2. **Large Systems**: For 100M+ entries, rebuild takes minutes
   - **Counter**: Can optimize (checkpoint index periodically, incremental rebuild)
   - **Counter**: Most systems won't have 100M entries

3. **Index is Small**: Why not persist it?
   - **Counter**: Adds complexity, consistency concerns, corruption risk

### Arguments AGAINST Persisting:

1. **Simplicity**: One less thing to maintain
2. **Consistency**: Index always matches logs (rebuild ensures it)
3. **No Corruption**: Can't have corrupted index if we don't persist it
4. **Focus**: Allows focusing on bigger picture questions

## Revised Design: In-Memory Index Only

### Implementation:

```go
type Storage struct {
    // ... other fields ...
    index *index.Index  // In-memory only, rebuilt on startup
}

func (s *Storage) Open() error {
    // Rebuild index from logs
    s.index = index.NewIndex("")
    if err := s.rebuildIndexFromLogs(); err != nil {
        return err
    }
    return nil
}

func (s *Storage) rebuildIndexFromLogs() error {
    // Scan all log files (level0.log, level1.log, etc.)
    for level := 0; level < maxLevel; level++ {
        logFile := fmt.Sprintf("level%d.log", level)
        entries, err := s.scanLogFile(logFile, level)
        if err != nil {
            return err
        }
        // Add entries to index
        for _, entry := range entries {
            seg := &index.LogSegment{
                RelPath:     pathFromSegment(entry.Path),
                StartCommit: entry.Commit,
                EndCommit:   entry.Commit,
                StartTx:     entry.Seq,
                EndTx:       entry.Seq,
                LogPosition: entry.LogPosition,  // Store position when scanning
            }
            s.index.Add(seg)
        }
    }
    return nil
}
```

### Startup Optimization (If Needed):

1. **Parallel Scanning**: Scan multiple log files in parallel
2. **Incremental Rebuild**: Only scan new entries since last checkpoint
3. **Checkpointing**: Periodically write "last scanned position" to disk (not full index)
4. **Lazy Loading**: Load index on-demand as paths are accessed

### When Would We Need Persisted Index?

**Only if:**
- Startup time > 1 minute is unacceptable
- System has 100M+ entries
- Can't optimize startup scanning

**But even then:**
- Can checkpoint "last scanned position" (not full index)
- Can do incremental rebuild
- Can optimize scanning (parallel, streaming)

## Revised Recommendation (Hybrid Approach)

**Persist index with max commit number:**
- ✅ Fast startup: Load persisted index (normal case)
- ✅ Incremental rebuild: Only scan logs from max commit forward
- ✅ Best of both worlds: Fast startup + incremental updates
- ✅ Consistency: Max commit number ensures we know where to resume

**How it works:**
1. **Normal shutdown**: Persist index + max commit number
2. **Normal startup**: Load index, check max commit, scan logs from that point forward
3. **Corruption/recovery**: If index corrupted or missing, full rebuild from logs

**Implementation:**
```go
type IndexMetadata struct {
    MaxCommit int64  // Highest commit number in index
    // Could also store: log file positions, etc.
}

// On shutdown
func (s *Storage) Close() error {
    // Persist index with metadata
    metadata := &IndexMetadata{MaxCommit: s.getMaxCommit()}
    return persistIndexWithMetadata("index.gob", s.index, metadata)
}

// On startup
func (s *Storage) Open() error {
    // Try to load persisted index
    index, metadata, err := loadIndexWithMetadata("index.gob")
    if err != nil {
        // No persisted index or corrupted - full rebuild
        return s.rebuildIndexFromLogs(0)  // Start from beginning
    }
    
    s.index = index
    
    // Incremental rebuild: scan logs from max commit forward
    return s.rebuildIndexFromLogs(metadata.MaxCommit + 1)
}
```

**Benefits:**
- ✅ Fast startup (load index, incremental scan)
- ✅ Handles normal case efficiently
- ✅ Falls back to full rebuild if needed
- ✅ Max commit number ensures correctness

**Tradeoffs:**
- ⚠️ Need to persist index (but we already have the code)
- ⚠️ Need to track max commit (but we already track this)
- ⚠️ Slightly more complex than no persistence, but much simpler than full persistence with consistency checks

## Open Questions

1. **Startup Performance**: What's acceptable startup time? (10 seconds? 1 minute?)
2. **Large Systems**: How to handle 100M+ entries? (Optimize scanning? Checkpoint?)
3. **Index Size**: What's the maximum index size we can tolerate in memory? (1GB? 10GB?)

## Conclusion

**Hybrid approach adopted**: Persist index with max commit number for fast startup, incremental rebuild from that point.

**Key insight**: We don't need to persist the index for correctness (logs are source of truth), but we can persist it for performance (fast startup). The max commit number ensures we know where to resume scanning.

**Benefits:**
- Fast startup (load persisted index)
- Incremental rebuild (only scan new entries)
- Falls back to full rebuild if index corrupted/missing
- Simpler than full persistence with consistency checks
