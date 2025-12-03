# Bottom-Up Implementation Approach

## Overview

This document outlines the bottom-up approach to implementing the storage system, acknowledging that circular dependencies exist at the **design level** but can be resolved by proceeding **bottom-up** in implementation.

## Key Insight

**Circular dependencies at design level are OK** if we:
1. Make design decisions upfront for all layers
2. Proceed bottom-up in implementation
3. Design each layer to accommodate higher layers

This avoids rework because design decisions accommodate future needs.

## Implementation Layers

### Layer 1: Foundation ✅ (Mostly Complete)

**Components**:
- Log entry format (Tony wire format, length-prefixed)
- Log file I/O (read/write at offsets)
- Index structure (`LogSegment`, hierarchical navigation)
- Path operations (kinded paths, extraction)

**Status**: ✅ Implemented

**Potential Addition**: May need `IsSnapshot bool` field in `LogSegment` to distinguish snapshots from diffs.

### Layer 2: Write Operations ⚠️ (Needs Design)

**Components**:
1. Transaction buffer → `LogEntry` conversion
2. Write `LogEntry` to log file (append, length-prefixed)
3. Update index (add `LogSegment`)
4. Atomic write guarantees

**Design Decisions**:
- Write code handles both diffs and snapshots (same `LogEntry` format)
- Index updates include `IsSnapshot` flag when writing snapshots

**Next Step**: Design write operations document

### Layer 3: Read Operations ⚠️ (Partially Designed)

**Components**:
1. Query index for segments (check `IsSnapshot` flag)
2. If snapshot exists ≤ commit: use as base state
3. Read remaining diffs after snapshot
4. Apply diffs sequentially
5. Extract path from root diff/snapshot

**Design Decisions**:
- Uses iterator API (`it.CommitsAt(commit, Down)`)
- Checks for snapshots (knows snapshot format from Layer 4 design)
- Falls back to reading all diffs if no snapshot
- Snapshot format: `LogEntry` with full state (design decision from Layer 4)

**Status**: Algorithm documented in `read_algorithm.md` (provisional - depends on snapshot format from Layer 4)

**Dependencies**: Requires Layers 1-2 (Foundation, Write)
**Design Dependency**: Needs snapshot format decision from Layer 4 (but can be designed together)

**Next Step**: Finalize read design (knows snapshot format, but snapshots won't exist until Layer 4 creates them)

### Layer 4: Compaction & Snapshots ⚠️ (Needs Design)

**Components**:
1. **Snapshot Format** (design decision):
   - Format: `LogEntry` with full state in `Diff` field
   - Same format as diffs (reuses write/read code)
   
2. **Snapshot Storage** (part of compaction):
   - Storage: Same log files as diffs
   - Indexing: `LogSegment` with `IsSnapshot=true`
   - Write: Uses Layer 2 (Write Operations) to write snapshots
   
3. **Compaction Operations**:
   - Determine compaction boundaries (alignment: `N % divisor^L == 0`)
   - Read all diffs in range `[startCommit, endCommit)` using Layer 3
   - Compute snapshot (full state at `endCommit`)
   - Write snapshot using Layer 2 (Write Operations)
   - Update index (add `LogSegment` with `IsSnapshot=true`)

**Design Decisions** (from `diff_composition_analysis.md`):
- Compaction produces **snapshots** (full state), not composed diffs
- Snapshots created at compaction boundaries
- Snapshot format: Same as `LogEntry` (reuses write/read code)
- Snapshot storage: Same log files (simplifies index)
- Snapshot indexing: `LogSegment.IsSnapshot` flag (minimal change)

**Dependencies**: Requires Layers 1-3 (Foundation, Write, Read)

**Key Insight**: Snapshot format can be designed independently (so read/write operations know what to expect), but snapshot storage/creation is compaction's responsibility.

**Next Step**: Design compaction document (includes snapshot format, storage, and creation)

## Design Decisions Needed

Before implementing Layer 2, we need to confirm these design decisions (all part of Layer 4 design):

### 1. Snapshot Format
**Decision**: Use `LogEntry` format with full state in `Diff` field
**Rationale**: Reuses existing code, fits naturally in index
**Needed for**: Layer 2 (Write Operations) and Layer 3 (Read Operations) to know what format to expect

### 2. Snapshot Storage
**Decision**: Store snapshots in same log files as diffs
**Rationale**: Simplifies index, natural ordering
**Needed for**: Layer 4 (Compaction) to know where to write snapshots

### 3. Snapshot Indexing
**Decision**: Add `IsSnapshot bool` to `LogSegment`
**Rationale**: Minimal change, easy to query
**Needed for**: Layer 3 (Read Operations) to find snapshots, Layer 4 (Compaction) to index them

### 4. Compaction Output
**Decision**: Compaction produces snapshots (full state at boundaries)
**Rationale**: From `diff_composition_analysis.md` - diffs can't be composed without state
**Needed for**: Layer 4 (Compaction) design

### 5. Compaction Trigger
**Decision**: At alignment boundaries (`N % divisor^L == 0`)
**Rationale**: Matches existing design assumptions
**Needed for**: Layer 4 (Compaction) design

**Key Point**: Snapshot format (decision #1) needs to be made before Layers 2-3, but snapshot storage/creation (decisions #2-5) are part of Layer 4 design.

## Implementation Order

1. **Confirm Design Decisions** (for Layer 4 - snapshot format, storage, compaction)
2. **Update Foundation** (Layer 1) - Add `IsSnapshot` if needed
3. **Design Write Operations** (Layer 2) - Accommodates snapshots (knows format)
4. **Design Read Operations** (Layer 3) - Handles snapshots (knows format, but snapshots don't exist yet)
5. **Design Compaction & Snapshots** (Layer 4) - Snapshot format, storage, and creation
6. **Implement Bottom-Up**:
   - Layer 2 → Layer 3 → Layer 4
   - Test each layer independently
   - Layer 3 can be tested without snapshots (they don't exist yet)
   - Layer 4 creates snapshots, Layer 3 can then use them

## Why This Works

1. **No Circular Dependencies in Code**: Each layer only depends on lower layers
2. **Design Decisions Accommodate Higher Layers**: No rework needed
3. **Incremental**: Can implement and test each layer independently
4. **Natural Progression**: Each layer builds on previous

## Next Steps

1. **Review and confirm design decisions** (snapshot format, storage, compaction)
2. **Update `LogSegment`** if needed (add `IsSnapshot` field)
3. **Design Layer 2** (Write Operations - knows snapshot format)
4. **Design Layer 3** (Read Operations - knows snapshot format, handles snapshots when they exist)
5. **Design Layer 4** (Compaction & Snapshots - snapshot format, storage, and creation)
6. **Implement bottom-up**: Layer 2 → Layer 3 → Layer 4
   - Layer 3 can be tested without snapshots (they don't exist until Layer 4 creates them)
