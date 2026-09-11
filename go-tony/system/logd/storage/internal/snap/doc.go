// Package snap provides event-based snapshot storage.
//
// Snapshots store stream.Event sequences with a size-bound index mapping
// paths to byte offsets. This enables efficient path lookups without
// loading entire documents into memory.
//
// A snapshot holds the events of one value: in logd, the whole document or the
// value at one path. [Builder] writes one from events; [Open] reads its header
// and index, and [Snapshot.ReadPath] and [Snapshot.ReadPathEventReader] seek
// through the index to the events of one path.
//
// # Format
//
//	[header: 12 bytes][events][index]
//
// The header is the size of the events in bytes (big-endian uint64) followed by
// the size of the index in bytes (big-endian uint32). The events are
// [stream.Event.WriteBinary] encodings, back to back. The index is an [Index] in
// Tony wire encoding: an entry for the root at offset 0 and, roughly every
// [GetChunkSize] bytes of events, one for the value that starts there, at the
// offset of its first event, counting the key and head comments that introduce
// it. Offsets are relative to the start of the events.
package snap
