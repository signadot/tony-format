# Design Dependency Cycle Analysis

## The Problem

We have a circular dependency:

```
Write Design ──┐
               │
               ▼
Read Design ───┼──► Compaction Design ──┐
               │                        │
               └────────────────────────┘
               │
               ▼
         Snapshot Design
```

**Dependencies**:
1. **Write Design** → needs to know what format to write (depends on read needs)
2. **Read Design** → needs to know how to read (depends on compaction/snapshots)
3. **Compaction Design** → needs to know what to produce (depends on write format and read needs)
4. **Snapshot Design** → needs to know format (depends on write) and usage (depends on read)

## Current State Assessment

### ✅ What's Designed

1. **Log Entry Format** (mostly complete):
   - `LogEntry` struct: `Commit`, `Seq`, `Timestamp`, `Diff`
   - Root diffs (all entries at root)
   - Length-prefixed entries
   - Tony wire format

2. **Index Structure** (complete):
   - Hierarchical index with `LogSegment`
   - Iterator API (`IterAtPath`, `CommitsAt`)
   - Path-based navigation

3. **Path Operations** (complete):
   - Kinded path package
   - Path extraction from root diffs
   - Path manipulation utilities

4. **Read Algorithm** (provisional):
   - Algorithm specified but depends on compaction
   - Uses iterators efficiently
   - References snapshots/compacted segments (not designed)

### ⚠️ What's Not Designed

1. **Compaction**:
   - What it produces (snapshots? composed diffs?)
   - When it runs (alignment-based? size-based?)
   - How it's stored (separate files? entries in logs?)
   - Format of compaction output

2. **Snapshots**:
   - Format (full state? path-specific?)
   - Storage (separate files? entries in logs?)
   - When created (at compaction boundaries?)
   - How indexed

3. **Write Operations**:
   - Transaction buffer → log entry conversion
   - Atomic writes
   - Index updates
   - Integration with compaction triggers

4. **Integration**:
   - How writes trigger compaction
   - How compaction updates index
   - How reads use snapshots
   - How all pieces fit together

## Approach: Bottom-Up Design with Circular Dependencies at Design Level

**Key Insight**: Circular dependencies at the **design level** are acceptable if we proceed **bottom-up** in implementation. We can make design decisions at each layer that accommodate higher layers, then implement from the bottom.

### Design vs Implementation

**Design Level** (circular dependencies OK):
- Write design ↔ Read design ↔ Compaction design ↔ Snapshot design
- All can be designed together, making decisions that accommodate each other

**Implementation Level** (bottom-up, no circular dependencies):
- Layer 1: Log entry format, basic I/O
- Layer 2: Write operations (uses Layer 1)
- Layer 3: Read operations (uses Layers 1-2)
- Layer 4: Compaction (uses Layers 1-3)
- Layer 5: Snapshots (uses Layers 1-4)

### Bottom-Up Implementation Order

**Layer 1: Foundation** (✅ mostly done)
1. Log entry format (Tony wire format, length-prefixed)
2. Log file I/O (read/write at offsets)
3. Index structure (LogSegment, hierarchical navigation)
4. Path operations (kinded paths, extraction)

**Layer 2: Write Operations** (needs design)
1. Transaction buffer → LogEntry conversion
2. Write LogEntry to log file (append)
3. Update index (add LogSegment)
4. Atomic write guarantees

**Layer 3: Read Operations** (partially designed)
1. Query index for segments
2. Read LogEntry from log file
3. Apply diffs sequentially
4. Extract path from root diff

**Layer 4: Compaction & Snapshots** (needs design)
1. **Snapshot Format** (design decision - needed by Layers 2-3):
   - Format: `LogEntry` with full state in `Diff` field
   
2. **Snapshot Storage** (part of compaction):
   - Storage: Same log files as diffs
   - Indexing: `LogSegment.IsSnapshot` flag
   
3. **Compaction Operations**:
   - Determine compaction boundaries (alignment)
   - Read all diffs in range (uses Layer 3)
   - Compute snapshot (full state)
   - Write snapshot (uses Layer 2)
   - Update index

**Key Insight**: Snapshot format can be designed independently (so Layers 2-3 know what to expect), but snapshot storage/creation is compaction's responsibility.

**Pros**:
- Each layer builds on previous (no circular dependencies in implementation)
- Can test incrementally
- Natural progression
- Design decisions accommodate higher layers (no rework needed)

**Cons**:
- Need to make design decisions upfront for higher layers
- May design things that aren't immediately needed

## Design Decisions Needed (Before Implementation)

To proceed bottom-up without rework, we need to make design decisions for higher layers now:

### 1. Snapshot Format Decision

**Question**: What format should snapshots use?

**Options**:
- **A**: Same as `LogEntry` (full state in `Diff` field, special `Commit` marker)
- **B**: Separate format (e.g., `SnapshotEntry` with full state)
- **C**: Separate files (e.g., `snapshot_<commit>.tony`)

**Recommendation**: **Option A** - Use `LogEntry` format with a flag or special commit marker. This:
- Reuses existing read/write code
- Fits naturally in index (same `LogSegment` structure)
- Simplifies implementation

**Design Decision Needed**: Confirm snapshot format.

### 2. Snapshot Storage Decision

**Question**: Where are snapshots stored?

**Options**:
- **A**: Same log files as diffs (interleaved)
- **B**: Separate snapshot files per level
- **C**: Separate snapshot directory

**Recommendation**: **Option A** - Same log files. This:
- Simplifies index (one structure for diffs and snapshots)
- Natural ordering (snapshot at commit X, then diffs after)
- Write code can handle both

**Design Decision Needed**: Confirm snapshot storage location.

### 3. Snapshot Indexing Decision

**Question**: How are snapshots found in the index?

**Options**:
- **A**: Special `LogSegment` with flag `IsSnapshot=true`
- **B**: Separate index structure
- **C**: Query by commit (snapshot if exists, else use diffs)

**Recommendation**: **Option A** - Add `IsSnapshot bool` to `LogSegment`. This:
- Minimal change to existing index
- Read algorithm can check flag
- Iterator can skip/find snapshots easily

**Design Decision Needed**: Confirm snapshot indexing approach.

### 4. Compaction Output Decision

**Question**: What does compaction produce?

**Answer** (from `diff_composition_analysis.md`): **Snapshots** (full state at boundaries).

**Design Decision Needed**: Confirm compaction produces snapshots, not composed diffs.

### 5. Compaction Trigger Decision

**Question**: When does compaction run?

**Options**:
- **A**: At alignment boundaries (N % divisor^L == 0)
- **B**: On-demand/background
- **C**: Size-based triggers

**Recommendation**: **Option A** - Alignment boundaries. This:
- Matches existing design assumptions
- Predictable, testable
- Can add size-based triggers later

**Design Decision Needed**: Confirm compaction trigger strategy.

## Concrete Next Steps (Bottom-Up)

### Step 1: Finalize Foundation (Layer 1)

**Status**: ✅ Mostly complete

**Remaining**:
- Confirm log entry format is final
- Verify index structure supports snapshots (may need `IsSnapshot` flag)

**Deliverable**: Confirm Layer 1 is complete

### Step 2: Design Write Operations (Layer 2)

**Goal**: Design write operations that accommodate snapshots.

**Design**:
- Transaction buffer → LogEntry conversion
- Write LogEntry to log file (append, length-prefixed)
- Update index (add LogSegment, support `IsSnapshot` flag)
- Atomic write guarantees
- **Accommodation**: Write code should handle both diffs and snapshots (same format)

**Deliverable**: Write design document

### Step 3: Design Read Operations (Layer 3)

**Goal**: Design read operations that use snapshots when available.

**Design**:
- Query index for segments (check `IsSnapshot` flag)
- If snapshot exists <= commit: use as base state
- Read remaining diffs after snapshot
- Apply diffs sequentially
- Extract path from root diff/snapshot

**Deliverable**: Read design document (update existing)

### Step 4: Design Compaction & Snapshots (Layer 4)

**Goal**: Design compaction that produces snapshots, including snapshot format, storage, and creation.

**Design**:
- **Snapshot Format** (needed by Layers 2-3):
  - Format: LogEntry with full state in `Diff` field
  - Same format as diffs (reuses write/read code)
  
- **Snapshot Storage** (part of compaction):
  - Storage: Same log files as diffs
  - Indexing: `LogSegment` with `IsSnapshot=true`
  
- **Compaction Operations**:
  - Trigger at alignment boundaries (N % divisor^L == 0)
  - Read all diffs in range [startCommit, endCommit) using Layer 3
  - Compute snapshot (full state at endCommit)
  - Write snapshot using Layer 2 (Write Operations)
  - Update index (add LogSegment with `IsSnapshot=true`)

**Key Point**: Snapshot format decision needs to be made before Layers 2-3, but snapshot storage/creation is compaction's responsibility.

**Deliverable**: Compaction design document (includes snapshot format, storage, and creation)

### Step 6: Implement Bottom-Up

**Order**:
1. Implement Layer 2 (Write Operations) - knows snapshot format, can write snapshots
2. Implement Layer 3 (Read Operations) - knows snapshot format, handles snapshots when they exist
3. Implement Layer 4 (Compaction & Snapshots) - creates snapshots, updates index
4. Test integration - Layer 3 can now use snapshots created by Layer 4

**Key**: 
- Each layer can be implemented and tested independently, building on previous layers
- Layer 3 can be tested without snapshots (they don't exist until Layer 4 creates them)
- Snapshot format decision needed before Layers 2-3, but snapshot creation is Layer 4's responsibility

## Key Principles

1. **Bottom-Up Implementation**: Each layer builds on previous layers (no circular dependencies in code)
2. **Design Decisions Upfront**: Make design decisions for higher layers before implementing lower layers (accommodates circular dependencies at design level)
3. **Incremental**: Can implement and test each layer independently
4. **No Rework**: Design decisions accommodate higher layers, so no refactoring needed
5. **Testable**: Each layer can be tested in isolation

## Current Recommendation

**Proceed Bottom-Up with Design Decisions Made**:

1. **Make Design Decisions** (for Layers 4-5):
   - Snapshot format: `LogEntry` with full state
   - Snapshot storage: Same log files as diffs
   - Snapshot indexing: `LogSegment.IsSnapshot` flag
   - Compaction output: Snapshots at boundaries
   - Compaction trigger: Alignment boundaries

2. **Update Foundation** (Layer 1):
   - Add `IsSnapshot bool` to `LogSegment` if needed
   - Confirm log entry format accommodates snapshots

3. **Design Write Operations** (Layer 2):
   - Write diffs and snapshots (same format)
   - Update index with `IsSnapshot` flag

4. **Design Read Operations** (Layer 3):
   - Use snapshots when available
   - Fall back to reading all diffs

5. **Design Compaction** (Layer 4):
   - Produces snapshots
   - Updates index

6. **Implement Bottom-Up**:
   - Layer 2 → Layer 3 → Layer 4 → Layer 5
   - Test each layer independently

**Why This Works**:
- Circular dependencies exist only at design level (OK)
- Implementation proceeds bottom-up (no circular dependencies in code)
- Design decisions accommodate higher layers (no rework needed)
- Each layer builds on previous (natural progression)
