# Compacted Snapshot Format: Pros and Cons Analysis

## The Proposal

**Question**: Should compacted snapshots (produced by compaction at higher levels) use the same format as level 0 log entries (diffs)?

**Current Design**:
- Level 0: `LogEntry` with `Diff` field containing incremental changes (diffs)
- Higher levels (compacted): `LogEntry` with `Diff` field containing full state (snapshots)
- **Same format**: Both use `LogEntry` struct, but `Diff` field has different semantics

**Alternative Considered**:
- Level 0: `LogEntry` with `Diff` field (diffs)
- Higher levels: Different format/structure for snapshots (e.g., separate `SnapshotEntry` type, or different file format)

## Option A: Same Format (LogEntry for Both)

**Proposal**: Compacted snapshots use the same `LogEntry` format as level 0 diffs.

**Structure**:
- Level 0: `LogEntry{Commit, Seq, Timestamp, Diff: <incremental changes>}`
- Higher levels: `LogEntry{Commit, Seq, Timestamp, Diff: <full state>}`
- Same struct, same wire format, same file format
- Distinguish via `IsSnapshot` flag in `LogSegment` index

### Pros

1. **Code Reuse**
   - Same read/write code for both diffs and snapshots
   - Same serialization/deserialization logic
   - Same file I/O operations
   - Less code duplication

2. **Simpler Implementation**
   - One format to handle everywhere
   - Write operations don't need to know if writing diff or snapshot
   - Read operations use same code path (just different interpretation of `Diff` field)

3. **Unified Index Structure**
   - `LogSegment` works for both diffs and snapshots
   - Same indexing logic
   - Just need `IsSnapshot` flag to distinguish

4. **Easier Testing**
   - Same test utilities work for both
   - Can reuse test helpers
   - Less test code duplication

5. **Flexibility**
   - Can change snapshot format without changing struct
   - Just change how `Diff` field is interpreted
   - Easier to evolve

6. **Consistent File Format**
   - Same log file structure at all levels
   - Same length-prefixed entry format
   - Easier to debug/inspect files

### Cons

1. **Semantic Confusion**
   - `Diff` field means different things (incremental vs full state)
   - Code must check `IsSnapshot` flag to know how to interpret
   - Potential for bugs if flag is wrong

2. **Type Safety**
   - No compile-time distinction between diff and snapshot
   - Must rely on runtime flag
   - Could accidentally treat snapshot as diff or vice versa

3. **Documentation Overhead**
   - Must document that `Diff` field semantics differ
   - Must document when `IsSnapshot` flag is set
   - More cognitive load for developers

4. **Index Overhead**
   - Need `IsSnapshot` flag in `LogSegment`
   - Must check flag in read operations
   - Extra field in index structure

## Option B: Different Format (Separate SnapshotEntry)

**Proposal**: Compacted snapshots use a different format/structure.

**Structure**:
- Level 0: `LogEntry{Commit, Seq, Timestamp, Diff: <incremental changes>}`
- Higher levels: `SnapshotEntry{Commit, Timestamp, State: <full state>}` (or similar)
- Different struct, potentially different wire format

### Pros

1. **Type Safety**
   - Compile-time distinction between diff and snapshot
   - Can't accidentally mix them
   - Type system enforces correct usage

2. **Clear Semantics**
   - `Diff` always means incremental changes
   - `State` (or `Snapshot`) always means full state
   - No ambiguity about what field means

3. **Optimized Format**
   - Can optimize snapshot format separately
   - Might not need `Seq` field (snapshots are at boundaries, not per-tx)
   - Can use different compression or encoding

4. **Better Documentation**
   - Clear separation in code
   - Easier to understand what each type represents
   - Less cognitive load

### Cons

1. **Code Duplication**
   - Need separate read/write code for snapshots
   - Separate serialization/deserialization
   - More code to maintain

2. **More Complex Implementation**
   - Two formats to handle
   - Write operations must know which format to use
   - Read operations must handle both formats
   - More code paths

3. **Index Complexity**
   - Might need different index structure for snapshots
   - Or unified index that handles both types
   - More complex indexing logic

4. **File Format Complexity**
   - Might need different file format or entry markers
   - Or type discriminator in entries
   - More complex file structure

5. **Testing Overhead**
   - Need separate test utilities for snapshots
   - More test code
   - More test cases

6. **Evolution Complexity**
   - Changes to format affect both types
   - Or must maintain compatibility for both
   - More complex versioning

## Comparison Table

| Aspect | Same Format (LogEntry) | Different Format (SnapshotEntry) |
|--------|------------------------|----------------------------------|
| **Code Reuse** | ✅ High (same code for both) | ❌ Low (separate code) |
| **Type Safety** | ❌ Runtime flag only | ✅ Compile-time types |
| **Semantic Clarity** | ❌ `Diff` field means different things | ✅ Clear separation |
| **Implementation Complexity** | ✅ Simpler (one format) | ❌ More complex (two formats) |
| **Index Complexity** | ⚠️ Medium (needs flag) | ⚠️ Medium (needs type handling) |
| **File Format** | ✅ Consistent | ⚠️ Might differ |
| **Testing** | ✅ Easier (reuse utilities) | ❌ More test code |
| **Evolution** | ✅ Easier (one format) | ❌ More complex (two formats) |
| **Documentation** | ⚠️ Must document flag semantics | ✅ Clearer (separate types) |
| **Flexibility** | ✅ High (change interpretation) | ⚠️ Medium (change both types) |

## Key Questions

1. **How often will snapshot format need to change?**
   - If rarely: Same format is fine
   - If often: Separate format allows independent evolution

2. **How important is type safety?**
   - Very important: Separate format wins
   - Less important: Same format is simpler

3. **How much code reuse matters?**
   - High reuse value: Same format wins
   - Less important: Separate format acceptable

4. **What's the performance impact?**
   - Same format: No performance difference
   - Different format: Might allow optimizations

5. **What's the maintenance burden?**
   - Same format: Less code, simpler
   - Different format: More code, but clearer

## Recommendation

**Recommendation: Same Format (LogEntry for Both)**

**Rationale**:
1. **Code reuse** is valuable - less code to maintain
2. **Implementation simplicity** - one format to handle
3. **Consistent file format** - easier to debug/inspect
4. **Flexibility** - can evolve without breaking changes
5. **Runtime flag is acceptable** - `IsSnapshot` flag provides necessary distinction

**Mitigations for Cons**:
- Use clear naming/documentation for `Diff` field semantics
- Add validation to ensure `IsSnapshot` flag is set correctly
- Consider type aliases or wrapper types for clarity (but same underlying struct)

**However**, consider **Different Format** if:
- Type safety is critical (e.g., safety-critical systems)
- Snapshot format needs significant optimization
- Clear semantic separation is more important than code reuse

## Implementation Impact

### If Same Format (LogEntry):

**Changes Needed**:
1. Add `IsSnapshot bool` to `LogSegment`
2. Write operations: Use same `LogEntry` format, set `IsSnapshot` in index
3. Read operations: Check `IsSnapshot` flag, interpret `Diff` accordingly
4. Compaction: Write `LogEntry` with full state in `Diff` field

**Code Structure**:
```go
// Same struct for both
type LogEntry struct {
    Commit    int64
    Seq       int64
    Timestamp string
    Diff      *ir.Node  // Incremental changes OR full state
}

// Index distinguishes via flag
type LogSegment struct {
    // ... other fields
    IsSnapshot bool  // true = full state, false = incremental
}
```

**Benefits**:
- Reuse existing read/write code
- Same file format everywhere
- Simpler implementation

### If Different Format (SnapshotEntry):

**Changes Needed**:
1. Create new `SnapshotEntry` struct
2. Write operations: Different code paths for diffs vs snapshots
3. Read operations: Handle both formats
4. Compaction: Write `SnapshotEntry` instead of `LogEntry`
5. Index: Handle both entry types

**Code Structure**:
```go
// Different structs
type LogEntry struct {
    Commit    int64
    Seq       int64
    Timestamp string
    Diff      *ir.Node  // Always incremental changes
}

type SnapshotEntry struct {
    Commit    int64
    Timestamp string
    State     *ir.Node  // Always full state
}
```

**Benefits**:
- Type safety
- Clear semantics
- Potential for optimization

**Costs**:
- More code to maintain
- More complex implementation
- More test code

## Critical Issue: Monotonicity Violation

**Problem**: Log files are append-only and monotonic in commit order. If snapshots are written to the same log file as diffs, they break monotonicity.

**Example**:
```
level0.log:
  commit 1: diff
  commit 2: diff
  ...
  commit 15: diff
  commit 16: diff
  commit 17: diff  ← written before compaction
  commit 18: diff
  ...
  [compaction happens for commit 16]
  commit 16: snapshot  ← written AFTER commits 17, 18, etc.
```

**Issue**: Information about commit 16 appears after information about commits 17, 18, etc. This breaks the monotonic property.

### Solutions

#### Solution 1: Separate Snapshot Files

**Approach**: Snapshots go to separate files per level.

**Structure**:
- `level0.log`: Only diffs (monotonic)
- `level1.log`: Only diffs (monotonic)
- `level1.snapshots.log`: Snapshots for level 1 (written at compaction boundaries)
- `level2.log`: Only diffs (monotonic)
- `level2.snapshots.log`: Snapshots for level 2 (written at compaction boundaries)

**Pros**:
- ✅ Maintains monotonicity in each file
- ✅ Clear separation of diffs and snapshots
- ✅ Can optimize snapshot file format separately
- ✅ Easier to reason about file structure

**Cons**:
- ❌ More files to manage
- ❌ Read operations must check multiple files
- ❌ More complex file I/O

#### Solution 2: Snapshots Only in Higher Level Files

**Approach**: Level 0 only has diffs. Higher levels have snapshots written at compaction boundaries (before new diffs).

**Structure**:
- `level0.log`: Only diffs (monotonic, written in commit order)
- `level1.log`: Snapshots written at boundaries, then diffs after (monotonic within level)
- `level2.log`: Snapshots written at boundaries, then diffs after (monotonic within level)

**Example**:
```
level1.log:
  commit 16: snapshot  ← written when compaction happens
  commit 17: diff      ← written later, but in level 1
  commit 18: diff
  ...
  commit 32: snapshot  ← written when next compaction happens
  commit 33: diff
```

**Pros**:
- ✅ Maintains monotonicity (snapshots written before new diffs at that level)
- ✅ One file per level (simpler)
- ✅ Natural ordering (snapshot at boundary, then diffs)

**Cons**:
- ⚠️ Higher level files mix snapshots and diffs
- ⚠️ Must ensure snapshots are written before new diffs at that level

#### Solution 3: Snapshots in Separate Higher Level Files

**Approach**: Each level has two files - one for diffs, one for snapshots.

**Structure**:
- `level0.log`: Only diffs (monotonic)
- `level1.diffs.log`: Diffs for level 1 (monotonic)
- `level1.snapshots.log`: Snapshots for level 1 (written at boundaries)
- `level2.diffs.log`: Diffs for level 2 (monotonic)
- `level2.snapshots.log`: Snapshots for level 2 (written at boundaries)

**Pros**:
- ✅ Maintains monotonicity in each file
- ✅ Clear separation
- ✅ Can optimize formats separately

**Cons**:
- ❌ More files (2 per level)
- ❌ More complex file management
- ❌ Read operations must check multiple files per level

#### Solution 4: Pre-allocate Snapshot Slots

**Approach**: When compaction boundary is reached, write snapshot immediately (even if compaction hasn't happened yet).

**Structure**:
- `level0.log`: Diffs (monotonic)
- `level1.log`: Snapshots written at boundaries, then diffs (monotonic)

**Example**:
```
level1.log:
  commit 16: snapshot  ← written immediately when commit 16 is allocated
  [compaction happens later, fills in snapshot]
  commit 17: diff      ← written after snapshot
```

**Pros**:
- ✅ Maintains monotonicity
- ✅ One file per level

**Cons**:
- ❌ Must pre-allocate or reserve space
- ❌ More complex write logic
- ❌ Snapshot might be written before compaction completes

#### Solution 5: Double-Buffering (LogA/LogB) ⭐

**Approach**: Two log files per level, alternate between them for active writes and snapshots.

**Structure**:
- `level0.logA` and `level0.logB`: Alternate active log
- `level1.logA` and `level1.logB`: Alternate active log
- Active writes go to one log, snapshot appends to the other

**Example Flow**:
```
Initial: Active = LogA

Commits 1-15: Write diffs to level0.logA
Commit 16: Compaction boundary reached
  - Switch active writes to level0.logB
  - Snapshot level0.logA (compute full state from commits 1-16)
  - Append snapshot to level0.logA (after all diffs)
  - Continue writing commits 17+ to level0.logB

Commits 17-31: Write diffs to level0.logB
Commit 32: Next compaction boundary
  - Switch active writes to level0.logA
  - Snapshot level0.logB (compute full state from commits 17-32)
  - Append snapshot to level0.logB (after all diffs)
  - Continue writing commits 33+ to level0.logA
```

**File Structure**:
```
level0.logA:
  commit 1: diff
  commit 2: diff
  ...
  commit 16: diff
  commit 16: snapshot  ← appended after all diffs, maintains monotonicity

level0.logB:
  commit 17: diff
  commit 18: diff
  ...
  commit 32: diff
  commit 32: snapshot  ← appended after all diffs

level0.logA: (active again)
  commit 33: diff
  commit 34: diff
  ...
```

**Pros**:
- ✅ **Maintains monotonicity**: Snapshot always appended after all diffs in that log
- ✅ **No ordering issues**: Active log always has diffs in commit order
- ✅ **Simple snapshot write**: Just append to inactive log
- ✅ **Natural separation**: Active vs inactive log is clear
- ✅ **Same format**: Can use LogEntry for both diffs and snapshots
- ✅ **No pre-allocation**: Snapshot written when compaction completes
- ✅ **Parallel compaction**: Can snapshot inactive log while writing to active log

**Cons**:
- ❌ **Two files per level**: More files to manage
- ❌ **Read complexity**: Must check both logs to find segments
- ❌ **Index complexity**: Index must track which log contains which commits
- ❌ **Switch overhead**: Must coordinate log switching (but minimal)

**Key Insight**: Similar to double-buffering in graphics - one buffer for writing (active), one buffer for reading/snapshotting (inactive). Switch roles when snapshot is needed.

## Recommendation

**Recommendation: Solution 5 (Double-Buffering with LogA/LogB)** ⭐

**Rationale**:
1. **Maintains monotonicity perfectly**: Snapshot always appended after all diffs in that log
2. **No ordering issues**: Active log always has diffs in commit order
3. **Simple snapshot write**: Just append to inactive log (no coordination needed)
4. **Natural separation**: Active vs inactive log is clear
5. **Parallel compaction**: Can snapshot inactive log while writing to active log
6. **Same format**: Can use LogEntry for both diffs and snapshots

**Implementation**:
- Each level has two log files: `levelN.logA` and `levelN.logB`
- Track which log is active (e.g., `activeLog = "A"` or `"B"`)
- When compaction boundary is reached:
  1. Switch active log (A → B or B → A)
  2. Compute snapshot from inactive log (all diffs up to boundary)
  3. Append snapshot to inactive log (after all diffs)
  4. Continue writing new diffs to active log
- Index tracks which log contains which commits

**Key Benefits**:
- ✅ **No monotonicity violations**: Snapshot always comes after diffs in same log
- ✅ **No write blocking**: Can write to active log while snapshotting inactive log
- ✅ **Simple logic**: Just alternate between two logs
- ✅ **Same format**: LogEntry works for both diffs and snapshots

**Trade-offs**:
- ⚠️ Two files per level (but simpler logic)
- ⚠️ Read operations must check both logs (but index can optimize this)
- ⚠️ Index must track log file (A or B) for each segment

**Alternative Consideration**: Solution 2 (Snapshots in Higher Level Files) is simpler (one file per level) but requires careful ordering to ensure snapshots are written before new diffs. Double-buffering eliminates this concern entirely.

## Updated Format Decision

Given the monotonicity constraint and double-buffering approach:

**Same Format (LogEntry)**: ✅ Still recommended
- All levels: `LogEntry` with `Diff` field
- Level 0 active logs: `Diff` contains incremental changes
- Snapshot entries: `Diff` contains full state
- Same struct, same format, just different interpretation
- `IsSnapshot` flag in index distinguishes

**File Structure (Double-Buffering)**:
- `level0.logA` and `level0.logB`: Alternate active log, snapshots appended to inactive log
- `level1.logA` and `level1.logB`: Alternate active log, snapshots appended to inactive log
- `level2.logA` and `level2.logB`: Alternate active log, snapshots appended to inactive log

**Read Operations**:
- Check `IsSnapshot` flag to know if `Diff` contains incremental changes or full state
- Check which log (A or B) contains the segment (from index)
- Use snapshot as base state, then apply remaining diffs from same log or active log

**Index Structure**:
```go
type LogSegment struct {
    // ... existing fields
    IsSnapshot bool
    LogFile    string  // "A" or "B" - which log file contains this segment
}
```

**Implementation Details**:

1. **Active Log Tracking**:
   ```go
   type LevelState struct {
       ActiveLog string  // "A" or "B"
       LastSnapshotCommit int64  // Last commit that was snapshotted
   }
   ```

2. **Write Flow**:
   ```go
   // Write diff to active log
   activeLog := getActiveLog(level)
   logFile := fmt.Sprintf("level%d.log%s", level, activeLog)
   // Append diff to logFile
   // Update index with LogFile = activeLog
   ```

3. **Compaction Flow**:
   ```go
   // When compaction boundary reached (e.g., commit 16)
   inactiveLog := getInactiveLog(level)  // "B" if active is "A"
   // Read all diffs from inactiveLog up to boundary
   // Compute snapshot (full state)
   // Append snapshot to inactiveLog (after all diffs)
   // Update index: add LogSegment with IsSnapshot=true, LogFile=inactiveLog
   // Switch active log: activeLog = inactiveLog
   ```

4. **Read Flow**:
   ```go
   // Query index for segments
   // For each segment, check LogFile to know which log to read from
   // If IsSnapshot=true, use as base state
   // Otherwise, apply as diff
   ```

**Advantages Over Other Solutions**:

1. **vs Solution 1 (Separate Snapshot Files)**:
   - ✅ Simpler: Two files instead of N files (diffs + snapshots)
   - ✅ Natural alternation pattern
   - ✅ No need to decide which file to write to

2. **vs Solution 2 (Snapshots in Higher Level Files)**:
   - ✅ No ordering coordination needed
   - ✅ Snapshot always comes after diffs (guaranteed by design)
   - ✅ Can snapshot in parallel with writes

3. **vs Solution 3 (Separate Diff/Snapshot Files)**:
   - ✅ Simpler: Two files instead of two types of files
   - ✅ Natural alternation

**Considerations**:

1. **Level 0**: Double-buffering works perfectly
   - Active log gets diffs in commit order
   - Inactive log gets snapshot appended after all diffs

2. **Higher Levels**: Same pattern applies
   - Level 1: Snapshot from level 0, then diffs from level 0 compaction
   - Level 2: Snapshot from level 1, then diffs from level 1 compaction
   - Each level alternates between LogA and LogB

3. **Read Performance**:
   - Must check both logs (but index tells us which one)
   - Can optimize: if reading recent commits, likely in active log
   - Can optimize: if reading old commits, check inactive log first (has snapshot)

4. **Index Size**:
   - Adds one field (`LogFile`) per segment
   - Minimal overhead (1 byte per segment)

5. **File Management**:
   - Two files per level (but simpler logic)
   - Can archive/compress inactive logs if needed
   - Can delete old inactive logs after compaction to higher levels

**Example Timeline**:

```
Time 0: Active = A
  Commits 1-15: Write to level0.logA

Time 1: Commit 16 (boundary)
  - Switch: Active = B
  - Snapshot level0.logA (commits 1-16) → append to level0.logA
  - Commits 17+: Write to level0.logB

Time 2: Commits 17-31: Write to level0.logB

Time 3: Commit 32 (boundary)
  - Switch: Active = A
  - Snapshot level0.logB (commits 17-32) → append to level0.logB
  - Commits 33+: Write to level0.logA

Time 4: Level 1 compaction (when level 0 has enough)
  - Read snapshots from level0.logA and level0.logB
  - Compute level 1 snapshot
  - Write to level1.logA (or level1.logB, depending on active)
```

**Conclusion**: Double-buffering elegantly solves the monotonicity problem while maintaining simplicity. It's similar to graphics double-buffering - one buffer for writing, one for reading/snapshotting, then swap roles.

## Question: Do We Need Multiple Levels?

**Current Design**: Multiple levels (level0, level1, level2, ...) with compaction moving data between levels.

**Alternative**: Single level (level0) with snapshots at exponential boundaries.

### Option A: Single Level with Snapshots

**Structure**:
- `level0.logA` and `level0.logB`: Double-buffered logs
- Snapshots created at exponential boundaries: commit 16, 32, 64, 128, ...
- All diffs and snapshots in level 0

**Read Algorithm**:
1. Find highest snapshot ≤ commit (e.g., snapshot at commit 64 for commit 100)
2. Read snapshot from level0.logA or level0.logB
3. Apply remaining diffs after snapshot (commits 65-100)

**Pros**:
- ✅ **Simpler**: Only one level to manage
- ✅ **No level coordination**: Don't need to move data between levels
- ✅ **Simpler compaction**: Just create snapshot, no level promotion
- ✅ **Same read performance**: Still use snapshot + remaining diffs
- ✅ **Fewer files**: Only 2 files total (level0.logA, level0.logB)

**Cons**:
- ❌ **Larger level 0 files**: All history in one level
- ❌ **More segments to check**: Index might have more segments for level 0
- ❌ **No natural organization**: Can't separate recent vs old data

### Option B: Multiple Levels (Current Design)

**Structure**:
- `level0.logA` and `level0.logB`: Recent diffs + snapshots
- `level1.logA` and `level1.logB`: Older snapshots (from level 0 compaction)
- `level2.logA` and `level2.logB`: Even older snapshots (from level 1 compaction)
- Each level has snapshots at its own exponential boundaries

**Read Algorithm**:
1. Determine starting level (highest level with snapshot ≤ commit)
2. Read snapshot from that level
3. Apply remaining diffs from that level and lower levels

**Pros**:
- ✅ **Natural organization**: Recent data in level 0, older in higher levels
- ✅ **Smaller files per level**: Each level has bounded size
- ✅ **Faster index lookups**: Fewer segments per level to check
- ✅ **Can archive old levels**: Can compress/archive level 2+ independently

**Cons**:
- ❌ **More complex**: Must coordinate between levels
- ❌ **More files**: 2 files per level (but bounded number of levels)
- ❌ **Level promotion**: Must move snapshots between levels

### Analysis: Do We Need Multiple Levels?

**Key Question**: What's the purpose of multiple levels?

**Traditional LSM Tree Reasons**:
1. **Reduce read cost**: Fewer files to read
2. **Reduce write amplification**: Merge multiple files
3. **Organize by recency**: Recent data separate from old data

**For Our Use Case**:
1. **Read cost**: Single level with snapshots already optimizes reads (snapshot + remaining diffs)
2. **Write amplification**: We're not merging files, just creating snapshots
3. **Organization**: Do we need to separate recent vs old data?

**If compaction is just "create snapshot at boundary"**:
- No need to move data between levels
- No need for level promotion
- Just create snapshots at exponential boundaries in level 0

**However, multiple levels might still be useful for**:
- **Bounded file size**: Each level has bounded size (e.g., level 0: commits 0-1024, level 1: commits 1024-2048)
- **Archival**: Can archive/compress old levels independently
- **Index efficiency**: Fewer segments per level to check

### Recommendation

**Consider Single Level First** (Option A):

**Rationale**:
1. **Simpler**: If compaction is just snapshotting, no need for multiple levels
2. **Same read performance**: Snapshot + remaining diffs works the same
3. **Fewer files**: Only 2 files instead of 2N files
4. **Easier to implement**: No level coordination needed

**Structure**:
```
level0.logA: commits 1-16 (diffs) + commit 16 (snapshot)
level0.logB: commits 17-32 (diffs) + commit 32 (snapshot)
level0.logA: commits 33-64 (diffs) + commit 64 (snapshot)
level0.logB: commits 65-128 (diffs) + commit 128 (snapshot)
...
```

**Read Algorithm**:
1. Find highest snapshot ≤ commit (check both logs)
2. Read snapshot
3. Apply remaining diffs after snapshot

**If Single Level Becomes Problematic**:
- File size too large? → Add levels
- Index too slow? → Add levels
- Need archival? → Add levels

**But start simple**: Single level with snapshots at exponential boundaries.

### Updated Design

**Single Level with Double-Buffering**:
- `level0.logA` and `level0.logB`: Double-buffered
- Snapshots at exponential boundaries: 16, 32, 64, 128, 256, ...
- All diffs and snapshots in level 0
- Read: Find snapshot ≤ commit, apply remaining diffs

**Index Structure**:
```go
type LogSegment struct {
    StartCommit int64
    EndCommit   int64
    // ... other fields
    IsSnapshot bool
    LogFile    string  // "A" or "B"
    // No Level field needed - always level 0
}
```

**Compaction**:
- When boundary reached (e.g., commit 16):
  1. Switch active log
  2. Read all diffs from inactive log up to boundary
  3. Compute snapshot (full state)
  4. Append snapshot to inactive log
  5. Continue writing to active log

**No level promotion needed** - just create snapshots in place.

## Updated Recommendation: Single Level

Given that compaction is just creating snapshots at exponential boundaries, **single level is sufficient**. See `single_level_vs_multi_level.md` for detailed analysis.

**Final Structure**:
- Single level (level 0) with double-buffering
- `level0.logA` and `level0.logB`: Alternate active log
- Snapshots at exponential boundaries: 16, 32, 64, 128, 256, ...
- All diffs and snapshots in level 0
- Read: Find snapshot ≤ commit, apply remaining diffs

**Benefits**:
- ✅ Simpler (no level coordination)
- ✅ Same read performance (snapshot + remaining diffs)
- ✅ Fewer files (only 2 files)
- ✅ Easier to implement (no level promotion)

**Can add levels later** if needed (e.g., for archival or bounded file size), but start simple.

## Conclusion

**Same format (LogEntry for both)** is still recommended, BUT:
- Snapshots must be written to higher level files (not level 0)
- Snapshots written at compaction boundaries, before new diffs at that level
- This maintains monotonicity while keeping format consistent

The semantic difference (diff vs full state) is handled by the `IsSnapshot` flag in the index, which is checked at read time to determine how to interpret the `Diff` field.
