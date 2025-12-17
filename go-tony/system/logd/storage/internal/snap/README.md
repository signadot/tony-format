# Event-Based Snapshot Design

## Overview

This package provides event-based snapshot storage for logd. Snapshots store events directly from the `stream` package, with a size-bound index for efficient path lookups.

## Design Goals

1. **Store events directly**: Snapshots contain a stream of `stream.Event` values, not IR nodes
2. **Size-bound index**: The index fits in memory even for very large snapshots
3. **Efficient path access**: Look up paths without reading the entire snapshot
4. **Streaming reads**: Support reading paths without full document reconstruction

## Architecture

### Snapshot Format

```
[8 bytes: event stream size (uint64, big-endian)]
[4 bytes: index size (uint32, big-endian)]
[event stream bytes]
[index bytes]
```

- **Event stream size**: 8-byte uint64 indicating size of event stream in bytes
- **Index size**: 4-byte uint32 indicating size of index in bytes
- **Event stream**: Sequence of `stream.Event` values encoded in Tony format
- **Index**: Size-bound map of paths to byte offsets

The sizes are written first (as placeholders, then updated), followed by the event stream and index.

### Index Structure

The index is a list of kpaths in order of the stream events, each associated with an offset in the event data:

- **IndexEntry**: Contains a kpath (string) and offset (int64)
- **Entries**: Ordered list of IndexEntry values, sorted by offset
- **Ancestor lookup**: If exact path not indexed, find nearest ancestor

### Building Snapshots

1. Write placeholder sizes at beginning (12 bytes: 8 for event stream size, 4 for index size)
2. Process IR node or event stream
3. Convert to events (if starting from IR node)
4. Write events to snapshot file
5. Build size-bound index while processing events
6. After all events written:
   - Write index bytes
   - Seek back to beginning and update event stream size and index size

### Reading Snapshots

1. Read sizes from beginning of file:
   - Read event stream size (8 bytes, uint64)
   - Read index size (4 bytes, uint32)
2. Read event stream (starting at offset 12, for event stream size bytes)
3. Read index (starting after event stream, for index size bytes)
4. Parse index structure
5. For path lookup:
   - Find path or nearest ancestor in index
   - Seek to offset in event stream
   - Decode events from that offset
   - Reconstruct path value from events

## Implementation Status

- [x] Basic index structure (Index, IndexEntry)
- [x] Event-based snapshot writer
- [x] Index builder (records paths and offsets as events are processed)
- [x] Snapshot reader with path lookup, basic

## Migration from IR-Node-Based Snapshots

The previous IR-node-based implementation (with `!snap-loc`, `!snap-range`, `!snap-chunks`) has been archived in `archive/`. The new design:

- **Simpler**: No chunking logic, just events + index
- **More efficient**: Direct event storage, no IR node conversion overhead
- **Better scaling**: Size-bound index works for arbitrarily large snapshots
