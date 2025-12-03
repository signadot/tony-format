# Storage Implementation Strategy

## Overview

This document provides the complete design and implementation strategy for the storage system, resolving circular dependencies through a bottom-up approach with upfront design decisions.

## The Problem: Circular Dependencies

At the design level, we have circular dependencies:

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

**Key Insight**: Circular dependencies at the **design level** are acceptable if we:
1. Make design decisions upfront for all layers
2. Proceed **bottom-up** in implementation
3. Design each layer to accommodate higher layers

This avoids rework because design decisions accommodate future needs.

## Solution: Bottom-Up Implementation

### Implementation Layers

Each layer builds on previous layers (no circular dependencies in code):

#### Layer 1: Foundation ✅ (Mostly Complete)

**Components**:
- Log entry format (Tony wire format, length-prefixed)
- Log file I/O (read/write at offsets)
- Index structure (`LogSegment`, hierarchical navigation)
- Path operations (kinded paths, extraction)
- **Token streaming API** (`TokenSource` and `TokenSink` from `go-tony/token`) ✅ **Available**

**Status**: ✅ Implemented (token streaming available)

**Token Streaming Capabilities**:
- ✅ `TokenSource`: Streaming tokenization from `io.Reader` (enables streaming reads)
- ✅ `TokenSink`: Streaming encoding with offset tracking (enables path indexing)
- ✅ Node start detection via callbacks (enables inverted index building)

**Potential Addition**: May need `IsSnapshot bool` field in `LogSegment` to distinguish snapshots from diffs.

#### Layer 2: Write Operations ⚠️ (Needs Design)

**Components**:
1. Transaction buffer → `LogEntry` conversion
2. Write `LogEntry` to log file (append, length-prefixed)
3. Update index (add `LogSegment`)
4. Atomic write guarantees

**Design Decisions**:
- Write code handles both diffs and snapshots (same `LogEntry` format)
- Index updates include `IsSnapshot` flag when writing snapshots
- **Needs**: Snapshot format decision (from Layer 4 design)

**Dependencies**: Requires Layer 1 (Foundation)

#### Layer 3: Read Operations ⚠️ (Partially Designed)

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
- **Needs**: Snapshot format decision (from Layer 4 design)

**Dependencies**: Requires Layers 1-2 (Foundation, Write)

**Status**: Algorithm documented in `read_algorithm.md` (provisional - depends on snapshot format)

#### Layer 4: Compaction & Snapshots ⚠️ (Needs Design)

**Components**:

1. **Snapshot Format** (design decision - needed by Layers 2-3):
   - Format: `LogEntry` with full state in `Diff` field
   - Same format as diffs (reuses write/read code)
   
2. **Snapshot Storage** (part of compaction):
   - Storage: Same log files as diffs
   - Indexing: `LogSegment` with `IsSnapshot=true`
   - Write: Uses Layer 2 (Write Operations) to write snapshots
   - **Token Streaming**: Use `TokenSink` for offset tracking and path indexing
   
3. **Compaction Operations**:
   - Determine compaction boundaries (alignment: `N % divisor^L == 0`)
   - Read all diffs in range `[startCommit, endCommit)` using Layer 3
   - Compute snapshot (full state at `endCommit`)
   - Write snapshot using Layer 2 (Write Operations) with `TokenSink` for indexing
   - Update index (add `LogSegment` with `IsSnapshot=true`)
   - **Build inverted index** using `TokenSink` callbacks during write

4. **Sub-Document Indexing** (enabled by token streaming):
   - Break large snapshots into sub-documents at natural path boundaries
   - Use `TokenSink` callbacks to detect node starts
   - Build inverted index: path → sub-document IDs and offsets
   - Read sub-documents using `TokenSource` with `io.SectionReader`

5. **Path Indexing** (enabled by token streaming):
   - Track byte offsets during snapshot write using `TokenSink.Offset()`
   - Detect node starts via `TokenSink` callbacks
   - Store path → offset mapping in index
   - Read paths from offsets using `TokenSource` with `io.SectionReader`

**Design Decisions** (from `diff_composition_analysis.md`):
- Compaction produces **snapshots** (full state), not composed diffs
- Snapshots created at compaction boundaries
- Snapshot format: Same as `LogEntry` (reuses write/read code)
- Snapshot storage: Same log files (simplifies index)
- Snapshot indexing: `LogSegment.IsSnapshot` flag (minimal change)
- **Token streaming enables**: Inverted index sub-documents, path indexing within snapshots

**Dependencies**: Requires Layers 1-3 (Foundation, Write, Read), Token streaming API

**Key Insight**: Snapshot format can be designed independently (so Layers 2-3 know what to expect), but snapshot storage/creation is compaction's responsibility. **Token streaming enables critical optimizations** for large snapshots (inverted index, path indexing).

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

## Implementation Flow

### Phase 1: Design Decisions

**Goal**: Make all design decisions upfront to avoid rework.

1. **Confirm snapshot format** (decision #1)
   - Needed before Layers 2-3
   - Format: `LogEntry` with full state

2. **Confirm snapshot storage/indexing** (decisions #2-3)
   - Part of Layer 4 design
   - Storage: Same log files
   - Indexing: `LogSegment.IsSnapshot` flag

3. **Confirm compaction design** (decisions #4-5)
   - Part of Layer 4 design
   - Output: Snapshots
   - Trigger: Alignment boundaries

### Phase 2: Foundation Updates

**Goal**: Ensure foundation supports all layers.

1. **Update `LogSegment`** if needed
   - Add `IsSnapshot bool` field
   - Update comparison functions if needed

2. **Verify log entry format**
   - Confirms it accommodates snapshots (full state in `Diff` field)

### Phase 3: Design Documents

**Goal**: Create detailed design documents for each layer.

1. **Design Layer 2** (Write Operations)
   - Transaction buffer → `LogEntry` conversion
   - Write to log file, update index
   - Handle both diffs and snapshots (same format)

2. **Design Layer 3** (Read Operations)
   - Query index, read diffs/snapshots
   - Apply diffs sequentially
   - Use snapshots when available

3. **Design Layer 4** (Compaction & Snapshots)
   - Snapshot format, storage, indexing
   - Compaction operations
   - Integration with write/read

### Phase 4: Implementation (Bottom-Up)

**Goal**: Implement each layer, building on previous layers.

**Order**:
1. **Implement Layer 2** (Write Operations)
   - Write diffs to log file
   - Update index
   - Can write snapshots (same format, but none exist yet)

2. **Implement Layer 3** (Read Operations)
   - Read diffs sequentially
   - Apply diffs
   - Check for snapshots (none exist yet, but code handles them)
   - **Can be tested**: Reads work without snapshots

3. **Implement Layer 4** (Compaction & Snapshots)
   - Create snapshots at compaction boundaries
   - Write snapshots using Layer 2
   - Update index with `IsSnapshot` flag
   - **Can be tested**: Creates snapshots, Layer 3 can now use them

**Testing Strategy**:
- Each layer can be tested independently
- Layer 3 can be tested without snapshots (they don't exist until Layer 4 creates them)
- Integration testing: Layer 3 uses snapshots created by Layer 4

## Why This Works

1. **No Circular Dependencies in Code**: Each layer only depends on lower layers
2. **Design Decisions Accommodate Higher Layers**: No rework needed
3. **Incremental**: Can implement and test each layer independently
4. **Natural Progression**: Each layer builds on previous
5. **Testable**: Each layer can be tested in isolation

## Current Status

### ✅ Complete
- Layer 1: Foundation (mostly complete)
- **Token streaming API available** (`TokenSource` and `TokenSink`)
- Design decisions identified
- Approach documented

### ⚠️ In Progress
- Layer 2: Write Operations (needs design)
- Layer 3: Read Operations (partially designed)
- Layer 4: Compaction & Snapshots (needs design)

### 📋 Next Steps

1. **Confirm design decisions** (snapshot format, storage, compaction)
2. **Update `LogSegment`** if needed (add `IsSnapshot` field)
3. **Design Layer 2** (Write Operations) - **integrate TokenSink for offset tracking**
4. **Design Layer 3** (Read Operations - finalize with snapshot support) - **integrate TokenSource for streaming reads**
5. **Design Layer 4** (Compaction & Snapshots) - **use token streaming for inverted index and path indexing**
6. **Implement bottom-up**: Layer 2 → Layer 3 → Layer 4

**Token Streaming Integration**:
- Layer 2: Use `TokenSink` for offset tracking during write
- Layer 3: Use `TokenSource` for streaming reads of large entries
- Layer 4: Use `TokenSink` callbacks to build inverted index, use `TokenSource` for sub-document reads

## Related Documents

- `DESIGN.md` - Overall system design
- `read_algorithm.md` - Detailed read algorithm (provisional)
- `diff_composition_analysis.md` - Why snapshots are necessary
- `compaction_snapshot_design.md` - Compaction design questions
- `design_dependency_analysis.md` - Detailed dependency analysis
- `bottom_up_approach.md` - Layer-by-layer breakdown

## Key Principles

1. **Bottom-Up Implementation**: Each layer builds on previous layers (no circular dependencies in code)
2. **Design Decisions Upfront**: Make design decisions for higher layers before implementing lower layers (accommodates circular dependencies at design level)
3. **Incremental**: Can implement and test each layer independently
4. **No Rework**: Design decisions accommodate higher layers, so no refactoring needed
5. **Testable**: Each layer can be tested in isolation
