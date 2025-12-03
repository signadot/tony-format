# Storage Redesign Documentation

## Primary Documents

### 📘 DESIGN.md
**The definitive design reference.** Use this for:
- Understanding the final design
- Implementation reference
- Architecture decisions
- API specifications

### 📋 implementation_plan.md
**Step-by-step implementation guide.** Use this for:
- Implementation tasks
- Testing checkpoints
- Dependencies between steps
- Verification criteria

### 📦 kindedpath_package.md
**Kinded path package documentation.** Use this for:
- Understanding the kinded path package
- API reference
- Usage examples

### 📖 read_algorithm.md
**Detailed read operation specification.** Use this for:
- Complete read algorithm specification
- Step-by-step read flow
- Edge cases and error handling
- Implementation reference

**⚠️ Note**: Read algorithm references compaction/snapshots which are not yet designed. See `compaction_snapshot_design.md`.

### 🔄 compaction_snapshot_design.md
**Compaction and snapshot design questions.** Use this for:
- Understanding the dependency cycle
- Design questions that need answers
- Breaking the dependency cycle
- Next steps for compaction design

### 🧮 diff_composition_analysis.md
**Analysis of diff composition algebra.** Use this for:
- Understanding why direct diff composition is hard
- Why snapshots are necessary
- Algebraic properties of diffs
- Snapshot-based compaction rationale

### 🎯 implementation_strategy.md ⭐ **START HERE**
**Complete design and implementation strategy.** Use this for:
- Overview of the entire approach
- Layer structure (Foundation → Write → Read → Compaction & Snapshots)
- Design decisions needed before implementation
- Complete implementation flow (design → foundation → implementation)
- Why this approach works (resolves circular dependencies)

### 🔗 design_dependency_analysis.md
**Detailed dependency cycle analysis.** Use this for:
- Deep dive into circular dependencies
- Current state assessment (what's designed, what's not)
- Multiple methodologies considered
- Detailed design decisions

### 📐 bottom_up_approach.md
**Layer-by-layer breakdown.** Use this for:
- Detailed component breakdown for each layer
- Dependencies between layers
- Design decisions per layer

### 🔄 level0_snapshots_analysis.md
**Snapshot format and storage analysis.** Use this for:
- Pros/cons of same format (LogEntry) vs different format
- Monotonicity problem and solutions
- Double-buffering approach (LogA/LogB)
- Single level vs multiple levels discussion

### 📊 single_level_vs_multi_level.md
**Single level vs multiple levels analysis.** Use this for:
- Do we need multiple levels if compaction is just snapshotting?
- Comparison of single level vs multiple levels
- Recommendation: Start with single level

### 🔍 snapshot_path_indexing.md
**Path indexing within snapshots analysis.** Use this for:
- Can we index paths within snapshots at byte offsets?
- Tony format seeking support (wire format + tokenization + bracket counting)
- Controller use case: 10^6:1 ratio makes path indexing essential
- Implementation details and challenges
- Recommendation: Implement path indexing (high priority)

### 📚 json_indexing_survey.md
**Survey of how existing databases index JSON.** Use this for:
- How MongoDB, PostgreSQL, FoundationDB, RocksDB handle JSON indexing
- Comparison of document stores vs key-value stores
- Insights for our path indexing approach
- Our approach aligns with key-value stores (partial reads)

### 🔄 inverted_index_subdocuments.md ⭐ **CURRENT APPROACH**
**Inverted index with sub-document indexing.** Use this for:
- How to handle ONE giant virtual document (not many separate documents)
- Breaking snapshot into sub-documents at natural boundaries
- Using inverted index (like Elasticsearch) to find relevant sub-docs
- Size threshold strategy (index sub-docs up to certain size)
- Implementation details and design
- **This is our recommended approach** for the controller use case

## Supporting Documents

### 📊 current_state_summary.md
Tracks the current implementation state and what's been completed.

### 📚 design_decisions_archive.md
Archives the historical decision-making process. Useful for understanding why decisions were made, but not the primary reference.

## Archived Documents

The following documents are kept for historical reference but are superseded by `DESIGN.md`:

- `redesign_outline.md` - Original design outline (superseded)
- `redesign_summary.md` - Original summary (superseded)
- Various critique/analysis documents (archived in `design_decisions_archive.md`)

## Quick Start

1. **Read `DESIGN.md`** to understand the final design
2. **Read `implementation_plan.md`** to see what needs to be implemented
3. **Read `kindedpath_package.md`** to understand path operations
4. **Check `current_state_summary.md`** to see what's been done

## Design Changes

### RelPath Removed

**Change**: `RelPath` field has been removed from `LogSegment` as it was redundant with `KindedPath`.

**Rationale**: 
- In the new design, all entries are written at root (no "relative" concept)
- `RelPath` and `KindedPath` were always the same
- `KindedPath` is more descriptive (specifies what to extract from root diff)

**Updated Structure**:
```go
type LogSegment struct {
    StartCommit int64
    StartTx     int64
    EndCommit   int64
    EndTx       int64
    KindedPath  string  // Kinded path (used for querying and extraction; "" for root)
    LogPosition int64   // Byte offset in log file
}
```

See `relpath_analysis.md` for detailed analysis.

## Document Status

- ✅ **DESIGN.md** - Final design reference (complete)
- ✅ **implementation_plan.md** - Implementation guide (complete)
- ✅ **kindedpath_package.md** - Package documentation (complete)
- ✅ **read_algorithm.md** - Read operation specification (complete)
- ✅ **current_state_summary.md** - Implementation state (updated as work progresses)
- 📚 **design_decisions_archive.md** - Historical archive (complete)
