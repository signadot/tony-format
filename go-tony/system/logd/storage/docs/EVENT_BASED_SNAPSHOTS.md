# Event-Based Snapshot Design

## Overview

Snapshots are being redesigned to store events directly from the `stream` package, with a size-bound index for efficient path lookups. This replaces the previous IR-node-based approach with chunking (`!snap-loc`, `!snap-range`, `!snap-chunks`).

## Key Design Decisions

### 1. Store Events Directly

- Snapshots contain a stream of `stream.Event` values
- No conversion to IR nodes during storage
- Events are encoded in Tony format

### 2. Size-Bound Index

- Index maps paths to byte offsets in the event stream
- Index size is bounded (e.g., 1MB max) regardless of snapshot size
- For large snapshots, paths are sampled (sparse indexing)
- For small snapshots, all paths can be indexed (dense indexing)

### 3. Efficient Path Access

- Lookup path in index → get byte offset
- Seek to offset in event stream
- Decode events from that offset
- Reconstruct path value from events

## Snapshot Format

```
[8 bytes: event stream size (uint64, big-endian)]
[4 bytes: index size (uint32, big-endian)]
[event stream bytes (sequence of stream.Event values)]
[index bytes (serialized Index structure)]
```

The sizes are written first (as placeholders, then updated after writing the data), followed by the event stream and index.

## Index Structure

The index is a list of kpaths in order of the stream events, each associated with an offset in the event data. Each entry contains:
- **KPath**: Kinded path string (e.g., "a.b[0]", "users.123.name")
- **Offset**: Byte offset in the event stream where this path appears

Entries are maintained in sorted order by offset (matching the order of stream events).

### Index Building Strategy

The index builder processes events and records paths with their offsets:
- Track current path using `stream.State`
- When a path should be indexed, add an entry with the current path and current offset
- Entries are maintained in sorted order by offset

### Path Lookup

A path is found through the directory (`directory.go`, `seek.go`): one table per segment, binary-searched, and the entry at the end gives the value's exact byte range, which is all that is read. A segment a table does not hold ends the lookup there: the path is absent. The chunk index, a seek to the chunk before the path and a scan forward, is how a snapshot written without a directory is read, and only that.

## Building Snapshots

### From IR Node

1. Write a placeholder header at the beginning (`snap.HeaderSize`: the magic, the event stream size, the directory size, the root table's offset, the index size)
2. Convert IR node to event stream (using `stream` package encoder)
3. Write events to snapshot file
4. While processing events:
   - Track current path (using `stream.State`)
   - Sample paths based on size bound
   - Record offsets for sampled paths
5. After all events written, build index
6. Write index bytes
7. Seek back to beginning and update event stream size and index size

### From Event Stream

1. Write a placeholder header at the beginning (`snap.HeaderSize`)
2. Write events directly to snapshot file
3. Build index while writing events (track paths and offsets), and write each container's table of children to the directory as the container closes (`directory.go`)
4. After all events written, write the directory, then the index bytes
5. Seek back to beginning and fill in the header

A snapshot written before the directory existed has a 12-byte header (event stream size, index size) with the index directly after the events; `Open` reads both, and such a snapshot answers `ErrNoDirectory` to a table.

## Reading Snapshots

### Full Snapshot

1. Read sizes from beginning of file (8 bytes event stream size, 4 bytes index size)
2. Read event stream (starting at offset 12, for event stream size bytes)
3. Read index (starting after event stream, for index size bytes)
4. Decode events to reconstruct full document

### Path Lookup

1. Read the header
2. From the root table, find each segment of the path in turn; a missing one is the path absent
3. Read the entry's byte range of the event stream, less the key that introduces it
4. Reconstruct path value

A snapshot without a directory reads its index instead, seeks to the chunk at or before the path, and scans forward.

## Benefits Over IR-Node-Based Design

1. **Simpler**: No chunking logic, no `!snap-loc`/`!snap-range` tags
2. **More efficient**: Direct event storage, no IR conversion overhead
3. **Better scaling**: Size-bound index works for arbitrarily large snapshots
4. **Streaming-friendly**: Events are naturally streamable

## Implementation Plan

- [x] Basic index structure (`Index`, `IndexEntry`)
- [ ] Event-based snapshot writer (`WriteSnapshot`)
- [ ] Index builder (`BuildIndex` - records paths and offsets)
- [ ] Snapshot reader (`OpenSnapshot`)
- [ ] Path lookup implementation (`LookupPath`)
- [ ] Integration with `SnapshotReader` interface
- [ ] Tests for large snapshots (>1GB)

## Migration Notes

The previous IR-node-based implementation has been archived in:
- `internal/snap/archive/` - Implementation files
- `docs/archive/ir_node_snapshots/` - Documentation

The new design maintains the same `SnapshotReader` interface, so callers don't need to change.
