# Compaction and Snapshot Design

## Problem: Dependency Cycle

We have a circular dependency:
1. **Read algorithm** references compaction (compacted segments, starting level)
2. **Compaction** isn't designed yet
3. **Snapshots** should be part of compaction but aren't designed

**Current State**: Read algorithm assumes compaction exists but we don't know what it produces.

## Breaking the Cycle

### Option 1: Design Compaction First (Recommended)

Design compaction (including snapshots) before finalizing read algorithm.

**Steps**:
1. Design compaction strategy (what it produces, when it runs)
2. Design snapshot mechanism (full state at compaction boundaries)
3. Define compaction output format (how snapshots are stored)
4. Then finalize read algorithm to use compaction/snapshots

**Pros**: Read algorithm can be fully specified
**Cons**: Need to design compaction now

### Option 2: Read Without Compaction First

Design read algorithm to work without compaction, add compaction later.

**Steps**:
1. Finalize read algorithm assuming no compaction (read all diffs)
2. Design compaction as optimization layer on top
3. Update read algorithm to use compaction when available

**Pros**: Can finish read algorithm now
**Cons**: Read algorithm needs to be updated later

### Option 3: Minimal Compaction Interface

Define minimal compaction interface that read algorithm can depend on.

**Steps**:
1. Define abstract compaction interface (what read algorithm needs)
2. Finalize read algorithm using interface
3. Design concrete compaction implementation later

**Pros**: Can proceed with both in parallel
**Cons**: Interface might not match final implementation

## Recommended Approach: Option 1

Design compaction and snapshots first, then finalize read algorithm.

## Compaction Design Questions

1. **What does compaction produce?**
   - **Snapshots** (full state at boundaries) - **Recommended**: See `diff_composition_analysis.md`
   - Merged diffs? (reduces number of diffs but may increase size)
   - Both? (snapshots at boundaries, merged diffs between)

2. **When does compaction run?**
   - Alignment-based (when `commit % divisor^L == 0`)
   - Size-based (when level reaches threshold)
   - Time-based (periodic)

3. **What is the compaction output format?**
   - Separate snapshot files?
   - Snapshot entries in log files?
   - How are snapshots indexed?

4. **How do snapshots relate to compaction?**
   - **Snapshots are the foundation** - compaction produces snapshots
   - Compaction reduces number of diffs to apply
   - Snapshots reduce document size to which diffs apply (full state vs incremental)

## Key Insight: Diff Composition Algebra

**Question**: Can we compute diff from D[i] to D[i+divisor] without full document state?

**Answer**: Generally **no** - see `diff_composition_analysis.md` for detailed analysis.

**Why**: Diffs are partial and state-relative. To compose them, we need to know the state they're merging with.

**Implication**: 
- **Snapshots are necessary** for efficient compaction
- Compaction computes snapshots (full state) at boundaries
- Composed diffs can be computed from snapshots (not the other way around)
- **Snapshots are the mechanism, compaction is the optimization on top**

## Snapshot Design Questions

1. **What is a snapshot?**
   - Full state at a specific commit? (root snapshot)
   - Full state at a specific path at a commit? (path-specific snapshots)
   - How is it stored? (separate files, entries in logs, compressed?)

2. **When are snapshots created?**
   - **At compaction boundaries** (`commit % divisor^L == 0`) - **Recommended**
   - Periodically?
   - On-demand?

3. **How are snapshots used in reads?**
   - Read snapshot at boundary <= commit as base state
   - Apply remaining diffs on top (from snapshot commit to requested commit)
   - How do we know which snapshot to use? (alignment rules: highest level where commit % divisor^L == 0)

4. **Snapshot vs Composed Diff**:
   - Snapshots: Full state (larger, but enables efficient reads)
   - Composed diffs: Can be computed from snapshots if needed
   - **Recommendation**: Store snapshots, compute composed diffs on-demand if needed

## Next Steps

1. **Design compaction strategy** (what, when, how)
2. **Design snapshot mechanism** (what, when, how)
3. **Define compaction/snapshot output format**
4. **Update read algorithm** to use compaction/snapshots
5. **Update implementation plan** with compaction steps

## Current Read Algorithm Assumptions

The read algorithm currently assumes:
- `readCompactedSegment(kindedPath, level, commit)` exists
- Returns `*CompactedSegment` with:
  - `StartCommit int64`
  - `EndCommit int64`
  - `State *ir.Node` (full state at EndCommit)
- Segments with commits in `[StartCommit, EndCommit]` are covered

**These assumptions need to be validated against the final compaction design.**
