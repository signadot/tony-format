# Single Level vs Multiple Levels Analysis

## The Question

If compaction is just creating snapshots at exponential backoff times (commit 16, 32, 64, 128, ...), do we need multiple levels, or can we do everything in level 0?

## Current Design Assumption: Multiple Levels

**Structure**:
- Level 0: Recent diffs + snapshots
- Level 1: Older snapshots (from level 0 compaction)
- Level 2: Even older snapshots (from level 1 compaction)
- Each level has snapshots at its own exponential boundaries

**Read Algorithm**:
1. Determine starting level (highest level with snapshot ≤ commit)
2. Read snapshot from that level
3. Apply remaining diffs from that level and lower levels

## Alternative: Single Level with Snapshots

**Structure**:
- Level 0 only: All diffs + snapshots at exponential boundaries
- Double-buffering: `level0.logA` and `level0.logB`
- Snapshots at: commit 16, 32, 64, 128, 256, 512, ...

**Read Algorithm**:
1. Find highest snapshot ≤ commit (check both logs)
2. Read snapshot
3. Apply remaining diffs after snapshot

## Comparison

### Single Level Approach

**Pros**:
- ✅ **Simpler**: Only one level to manage
- ✅ **No level coordination**: Don't need to move data between levels
- ✅ **Simpler compaction**: Just create snapshot, no level promotion
- ✅ **Same read performance**: Still use snapshot + remaining diffs
- ✅ **Fewer files**: Only 2 files total (level0.logA, level0.logB)
- ✅ **No level promotion logic**: Compaction is just "create snapshot"

**Cons**:
- ❌ **Larger level 0 files**: All history in one level
- ❌ **More segments in index**: All segments in level 0 (but can optimize)
- ❌ **No natural separation**: Can't separate recent vs old data by level

### Multiple Levels Approach

**Pros**:
- ✅ **Natural organization**: Recent data in level 0, older in higher levels
- ✅ **Bounded file size**: Each level has bounded size (if we limit commits per level)
- ✅ **Faster index lookups**: Fewer segments per level to check
- ✅ **Can archive old levels**: Can compress/archive level 2+ independently
- ✅ **Matches LSM tree pattern**: Familiar structure

**Cons**:
- ❌ **More complex**: Must coordinate between levels
- ❌ **More files**: 2 files per level (but bounded number of levels)
- ❌ **Level promotion**: Must move snapshots between levels
- ❌ **More code**: Level management, promotion logic

## Key Question: What's the Purpose of Multiple Levels?

**Traditional LSM Tree Reasons**:
1. **Reduce read cost**: Fewer files to read
   - But: Single level with snapshots already optimizes reads (snapshot + remaining diffs)
2. **Reduce write amplification**: Merge multiple files
   - But: We're not merging files, just creating snapshots
3. **Organize by recency**: Recent data separate from old data
   - Question: Do we need this separation?

**For Our Use Case**:
- **Read optimization**: ✅ Single level with snapshots achieves this
- **Write optimization**: ✅ Not applicable (we're not merging)
- **Organization**: ❓ Do we need it?

## Analysis: Do We Need Multiple Levels?

### If Compaction is Just "Create Snapshot at Boundary"

**Single Level Works**:
- Create snapshots at exponential boundaries: 16, 32, 64, 128, ...
- Read algorithm: Find snapshot ≤ commit, apply remaining diffs
- No need to move data between levels
- No need for level promotion

**Example**:
```
level0.logA:
  commits 1-16 (diffs)
  commit 16 (snapshot)

level0.logB:
  commits 17-32 (diffs)
  commit 32 (snapshot)

level0.logA:
  commits 33-64 (diffs)
  commit 64 (snapshot)

level0.logB:
  commits 65-128 (diffs)
  commit 128 (snapshot)
```

**Read at commit 100**:
1. Find snapshot ≤ 100: commit 64 snapshot in level0.logA
2. Read snapshot
3. Apply diffs: commits 65-100 from level0.logB

**Same performance as multiple levels!**

### When Multiple Levels Might Be Useful

1. **Bounded File Size**:
   - If we want to limit commits per level (e.g., level 0: 0-1024, level 1: 1024-2048)
   - But: With double-buffering, files naturally alternate, so size is bounded per file

2. **Archival**:
   - If we want to archive/compress old data independently
   - But: Can archive old log files (logA or logB) independently

3. **Index Efficiency**:
   - Fewer segments per level to check
   - But: Can optimize index with snapshot boundaries (group segments by snapshot)

4. **Level-Based Policies**:
   - Different retention policies per level
   - But: Can use commit-based policies instead

## Recommendation

**Start with Single Level** (Option A):

**Rationale**:
1. **Simpler**: If compaction is just snapshotting, no need for multiple levels
2. **Same read performance**: Snapshot + remaining diffs works the same
3. **Fewer files**: Only 2 files instead of 2N files
4. **Easier to implement**: No level coordination needed
5. **Easier to reason about**: All data in one place

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

**Compaction**:
- When boundary reached (e.g., commit 16):
  1. Switch active log
  2. Read all diffs from inactive log up to boundary
  3. Compute snapshot (full state)
  4. Append snapshot to inactive log
  5. Continue writing to active log

**No level promotion needed** - just create snapshots in place.

### If Single Level Becomes Problematic

**Add levels later if needed**:
- File size too large? → Add levels with bounded commits
- Index too slow? → Add levels to reduce segments per level
- Need archival? → Add levels for independent archival

**But start simple**: Single level with snapshots at exponential boundaries.

## Updated Design

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

## Conclusion

**Single level is sufficient** if compaction is just creating snapshots at exponential boundaries. Multiple levels add complexity without clear benefit for this use case.

Start with single level, add levels later if needed.
