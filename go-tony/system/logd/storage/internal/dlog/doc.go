// Package dlog provides double-buffered write-ahead logging.
//
// Two alternating log files (logA/logB) enable atomic switching during
// snapshot creation. Patches are appended to the active log.
//
// # Files
//
// A store's directory holds logA, logB and dlog.state. The state file is one line,
// "<active> <genA> <genB> <snapMark>" -- the active log, each file's generation, and how
// far into the active log the last snapshot reaches ([DLog.DeltaBytesSinceSnapshot]) --
// replaced through a temp file and a rename, both fsynced. [DLog.SwitchActive] flips which
// log is active; a snapshot ([DLog.NewSnapshotWriter]) and compaction
// ([DLog.CompactInactive]) write to the inactive one.
//
// # Records
//
// A record is a 4-byte big-endian length followed by an [Entry] encoded as binary
// stream events; a record in tony text form, as older logs hold, still decodes. A
// snapshot is a blob -- [BlobHeaderMagic], a 4-byte length, and the snap event stream --
// followed by the record of the Entry whose SnapPos points at the blob. An append is one
// positional write at the file's append frontier and is not synced: [DLog.Sync] forces a
// file to stable storage, and the storage package's durability setting decides when that
// is called.
//
// # Opening and walking
//
// Opening a log deletes nothing. [NewDLog] rolls back a compaction a crash interrupted,
// from the file's .old copy, and walks each file's frames to find the end of the last
// complete record: a torn tail is left in place and the next append overwrites it. A walk
// ([DLog.Iterator], [DLog.FileIterator]) is bounded by that append frontier, and a region
// it cannot read is stepped over to the next record that decodes; [DLogIter.Gaps] reports
// each such region.
//
// A compaction swaps the file for its rewrite and bumps the file's generation in one
// critical section under the file's lock. [DLog.ReadEntryAt] and [DLog.OpenReaderAt] take
// the generation a position was indexed under and ask it under that lock: the current
// generation reads the file, the one before the last compaction reads the file that
// compaction replaced -- kept open, unlinked, until the next -- and an older one is
// refused with [ErrCompactionInterrupted].
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
package dlog
