# Critique: 3-Piece Patch Design for Persistent Storage

## Scope

**In Scope**: Functional organization of persistent storage for patches
- How patches are simplified and stored
- How patches are applied during snapshot reads
- How patches are written during compaction
- How patches are written during snapshot building

**Out of Scope** (noted for future design):
- Caching strategies (higher-level concern)
- Compaction base reference details (separate design)
- In memory Performance optimization (separate concern)
- Transaction failure handling (already handled by tx system)
- ReadStateAt implementation details (separate)

## Overview

The design consists of 3 pieces for persistent storage:
1. **Coordinator simplifies patches** (write-time): Converts complex patches to simple ones before storage
2. **Streaming patches for snapshotting** (read-time): Applies simple patches when reading snapshots
3. **Streaming patches for compaction** (write-time): Writes patches instead of full snapshots

## Strengths ✅

### 1. Clear Functional Organization
- **Write-time**: Simplification happens once, before storage
- **Read-time**: Simple operations applied incrementally
- **Compaction-time**: Writes changes as patches

### 2. Memory Efficiency (Persistent Storage)
- **No container buffering**: Streaming is horizontal, respects memory constraints
- **Change-only storage**: Compaction writes patches (deltas), not full snapshots
- **Incremental processing**: Operations applied one at a time

### 3. Simplicity
- **Simple operations**: `!insert`, `!delete`, `!replace` are well-defined
- **Explicit operations**: PatchWriter receives explicit operation metadata
- **Chainable design**: Processors compose cleanly

## Design Critique: Within Scope

### 1. Coordinator Simplification (Write-Time)

**Current State** (from `coord.go`):
- Coordinator merges patches (line 209): `MergePatches(state.PatcherData)`
- Writes merged patch to storage log (line 245): `WriteAndIndex(..., mergedPatch, ...)`
- Patches are already stored in log

**Simplification Integration**:
- Simplification would happen between merge (line 209) and write (line 245)
- `simplifiedPatch := SimplifyPatches(mergedPatch)`
- Write simplified patch instead of merged patch

**Simplification Semantics** (clarified):
- **What simplification does**: Compute a diff at the path after the patch is applied and remove the matching conditions (replace `from: null` with match-all)
- **How it works**: 
  1. Apply complex patch to base document (via `ReadStateAt`)
  2. Compute diff between base and result
  3. Remove matching conditions (replace `from:` fields with match-all)
- **Where simplification happens**: In coordinator before write (between merge and write)
- **Failure handling**: Simplification cannot fail (it's just a diff). If it does fail, transaction fails before write to log (consistent with existing tx failure handling)


**Not in Scope**:
- Performance optimization (separate concern)
- Caching (higher-level)
- Failure recovery (handled by tx system)

### 2. Streaming Patches for Snapshotting (Read-Time)

**Current State**:
- Patches are stored in the storage log (as now)
- Patches are indexed by the `index` package for lookup
- Patches are applied in order (maintained by storage log)

**Design Questions** (within scope):
- **How are patches applied?**: Chain processors, apply sequentially (as designed in `patch_streaming_coordination_design.md`)
- **How are patches retrieved?**: Lookup patches for snapshot from storage log via index

**Not in Scope**:
- Performance optimization (separate concern)
- Caching strategies (higher-level)
- ReadStateAt implementation (separate)

### 3. Streaming Patches for Compaction (Write-Time)

**Design Questions** (within scope):
- **Patch generation**: How does PatchWriter generate patch structure from operations?
- **Operation accumulation**: How are operations organized into patch tree?
- **Patch format**: Patches written to storage log (same format as now)

**Not in Scope**:
- Base reference identification (compaction design concern)
- Change detection strategy (compaction design concern)
- Compaction failure recovery (handled by compaction design)

## Deep Dive: Coordinator Simplification (Within Scope)

### Current Coordinator Flow

**From `coord.go`**:
1. Match evaluation (line 181): `evaluateMatches(state, commitOps.ReadStateAt, currentCommit)`
   - Uses `ReadStateAt` for match conditions (path-scoped)
2. Merge patches (line 209): `MergePatches(state.PatcherData)`
   - Structural merge, no base document needed
3. Write patch (line 245): `WriteAndIndex(..., mergedPatch, ...)`
   - Writes merged patch to storage log

### Simplification Integration Point

**Where simplification fits**:
- After merge (line 209), before write (line 245)
- `simplifiedPatch := SimplifyPatches(mergedPatch)`
- Write `simplifiedPatch` instead of `mergedPatch`

### Simplification Semantics (Clarified)

**What Simplification Does**:
1. Apply complex patch to base document (via `ReadStateAt`)
2. Compute diff between base and result
3. Remove matching conditions (replace `from:` fields with match-all, e.g., `from: null` → match-all)

**Integration**:
- Simplification requires base document access (via `ReadStateAt`)
- Fits cleanly into coordinator flow: after merge, before write
- Memory constraint: Depends on `ReadStateAt` implementation (out of scope for this critique)

**Failure Handling** (Clarified):
- Simplification cannot fail (it's just a diff computation)
- If simplification fails, transaction fails before write to log (consistent with existing tx failure handling)
- No need to handle both complex and simple patches (simplification always succeeds or tx fails)

**Not in Scope**:
- Performance impact (separate concern - simplification doesn't add I/O)
- Coordinator bottleneck (separate concern)
- Caching (higher-level)

## Design Constraints for Future Work

### How Does This Design Constrain Larger-Scope Questions?

**Compaction Base Reference**:
- 3-part design provides patch application mechanism
- Compaction design must determine: how to identify base, how to access base
- **Constraint**: Compaction must work with operation-based patch generation

**ReadStateAt Performance**:
- 3-part design applies patches during reads
- ReadStateAt implementation must provide efficient patch application
- **Constraint**: Sequential patch application is the mechanism

**Caching Strategies**:
- 3-part design stores patches persistently
- Caching can layer on top (cache simplified patches, cache patched snapshots)
- **Constraint**: Caching works with persistent patch storage

## Key Design Questions (Within Scope)

### 1. Simplification Semantics ✅ (Clarified)
- **What it does**: Compute diff after applying patch, remove matching conditions
- **How it works**: Apply complex patch → compute diff → remove `from:` conditions
- **Integration**: Requires base document (via `ReadStateAt`), fits between merge and write
- **Memory**: Depends on `ReadStateAt` implementation (out of scope)

### 2. Simplification Failure ✅ (Clarified)
- **Failure model**: Simplification cannot fail (it's just a diff)
- **If it fails**: Transaction fails before write to log (consistent with existing pattern)
- **Impact**: No need to handle both complex and simple patches

**Note**: Patch storage format is already established (patches stored in storage log, indexed by `index` package)

## Conclusion: 3-Part Design Critique

### Design Strengths (Within Scope)

1. **Clear functional organization**: Write-time simplification, read-time application, compaction-time patch writing
2. **Memory efficient**: No container buffering, streaming is horizontal
3. **Simple operations**: `!insert`, `!delete`, `!replace` are well-defined
4. **Chainable design**: Processors compose cleanly

### Design Questions (Within Scope) ✅ (Clarified)

1. **Simplification semantics**: ✅ Clarified
   - **What**: Compute diff after applying patch, remove matching conditions (`from:` → match-all)
   - **How**: Apply complex patch to base → compute diff → remove `from:` conditions
   - **Integration**: Requires base document (via `ReadStateAt`), fits between merge and write
   
2. **Simplification failure**: ✅ Clarified
   - **Model**: Simplification cannot fail (it's just a diff)
   - **If it fails**: Transaction fails before write to log (consistent with existing pattern)
   - **Impact**: No need to handle both complex and simple patches

**Note**: Patch storage is already established (storage log + index package). Patches are applied in order (maintained by log). Performance concerns are out of scope (no extra I/O, inevitable in diff-based store).

### Design Constraints for Future Work

The 3-part design provides:
- **Mechanism**: How patches are simplified, applied, and written
- **Constraints**: Sequential patch application, operation-based patch generation
- **Foundation**: For compaction design, caching strategies, performance optimization

**Out of Scope** (noted for future):
- Compaction base reference (compaction design)
- Performance optimization (separate concern - no extra I/O, inevitable in diff-based store)
- Caching strategies (higher-level)
- ReadStateAt implementation (separate)

**Established**:
- Patch storage: Storage log (as now)
- Patch ordering: Maintained by storage log (patches cannot arrive out of order)
- Patch lookup: Index package
- Contract: Simplified patches written to storage commit log
- Simplification semantics: Compute diff after applying patch, remove matching conditions
- Simplification failure: Cannot fail (it's just a diff); if it fails, tx fails before write

The design is **functionally sound** for persistent storage organization. All key design questions within scope have been clarified.
