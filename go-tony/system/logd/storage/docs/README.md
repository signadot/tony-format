# Storage Package Documentation

## Current Focus: Event-Based Snapshots

The snapshot system is being redesigned to use events directly from the `stream` package. See:

- **[EVENT_BASED_SNAPSHOTS.md](./EVENT_BASED_SNAPSHOTS.md)** - Design document for event-based snapshots
- **[CLEANUP_SUMMARY.md](./CLEANUP_SUMMARY.md)** - Summary of package cleanup

## Documentation Organization

### Active Documentation

- **Event-Based Snapshots**: `EVENT_BASED_SNAPSHOTS.md`
- **Cleanup Summary**: `CLEANUP_SUMMARY.md`
- **Stream Package**: Various docs about stream package design and migration

### Archived Documentation

- **IR-Node-Based Snapshots**: `archive/ir_node_snapshots/` - Old snapshot implementation docs
- **Old API Assessments**: `archive/` - TokenSink/TokenSource assessments

## Stream Package Documentation

The following documents relate to the stream package, which is now the foundation for snapshots:

- `stream_package_design.md` - Stream package design
- `stream_migration_overview.md` - Migration overview
- `stream_encoder_decoder_api_spec.md` - API specification
- `final_streaming_api.md` - Final API design

## Snapshot Interface

The `SnapshotReader` interface (in `snapshot_interface.go`) remains unchanged and will be implemented using the new event-based approach. See:

- `snapshot_interface.go` - Interface definition
- `snapshot_interface_unified.md` - Interface design documentation

## Implementation Status

- ✅ Stream package implemented
- ✅ Package cleanup completed
- ✅ Event-based design documented
- ⏳ Event-based snapshot implementation (in progress)
