# Package Cleanup Summary

## Overview

The snapshot package has been cleaned up for the new event-based snapshot design. The previous IR-node-based implementation has been archived, and the package is now ready for efficient work on the event-based redesign.

## What Was Archived

### Implementation Files (`internal/snap/archive/`)

- `from_ir.go` - IR node to snapshot conversion (old chunking logic)
- `from_snap.go` - Snapshot patching with IR nodes
- `load.go` - Loading `!snap-loc` and `!snap-range` nodes
- `size.go` - IR node size estimation
- `patch_categorize.go` - Patch categorization for chunked containers
- `patch_categorize_test.go` - Tests for patch categorization
- `snapshot_test.go` - Tests for IR-node-based snapshots
- `snapshot.go` - Old Snapshot interface (IR-node-based)
- `open.go` - Opening IR-node-based snapshots
- `snap.md` - Old snapshot format documentation
- `patching_design.md` - Old patching design documentation

### Documentation Files (`docs/archive/ir_node_snapshots/`)

- `snapshot_index_*.md` - Various index design documents
- `snapshot_implementation_plan.md` - Old implementation plan
- `snapshot_ir_node_index.md` - IR node indexing docs
- `path_by_path_snapshotting.md` - Path-based snapshotting docs
- `snap_index_*.md` - Snap index design docs
- `range_descriptors_*.md` - Range descriptor design docs
- `snapshot_interface_impl.md` - Old interface implementation guide
- `snapshot_interface_streaming_example.md` - Old streaming examples

## What Remains (Current State)

### Core Files

- `internal/snap/doc.go` - Updated package documentation
- `internal/snap/README.md` - Event-based snapshot design overview

Note: The index structure will be designed as part of the implementation.

### Interface Files

- `snapshot_interface.go` - SnapshotReader interface (unchanged, still valid)
- `snapshot_interface_unified.md` - Interface design documentation

### Documentation

- `docs/EVENT_BASED_SNAPSHOTS.md` - New event-based snapshot design
- `docs/CLEANUP_SUMMARY.md` - This file

### Stream Package Integration

- `stream/event.go` - Event types (ready to use)
- `stream/state.go` - Path tracking (ready to use)

## New Design Direction

### Event-Based Snapshots

- Store `stream.Event` values directly (not IR nodes)
- Size-bound index for path lookups
- Efficient streaming reads without full document reconstruction

### Key Files to Implement

1. **Snapshot Writer**: Write events to snapshot file, build index
2. **Snapshot Reader**: Read index, lookup paths, decode events
3. **Index Builder**: Build size-bound index from event stream

## Next Steps

1. Implement event-based snapshot writer
2. Implement size-bound index builder
3. Implement snapshot reader with path lookup
4. Integrate with existing `SnapshotReader` interface
5. Add tests for large snapshots

## Notes

- The `SnapshotReader` interface remains unchanged, so callers don't need updates
- The new design is simpler and more scalable than the IR-node-based approach
- All archived files are preserved for reference if needed
