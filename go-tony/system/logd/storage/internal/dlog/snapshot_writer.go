package dlog

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// A blob header stands where a record's length prefix would, and its first four bytes
// are a magic no record length takes. Two forms are read:
//
//	[BlobHeaderMagic64][uint64 length]  12 bytes, written since go-tony v0.0.217
//	[BlobHeaderMagic  ][uint32 length]   8 bytes, written before, read still
//
// The 32-bit length wrapped silently for a snapshot of 4 GiB or more, and the walk on
// open then read the log from the wrong offset (p478tacqh12krg32msn0 item 15). A log
// written before holds only the short form and opens as it did.

// BlobHeaderMagic marks the short, 32-bit-length blob header of older logs.
const BlobHeaderMagic uint32 = 0xFFFFFFFF

// BlobHeaderSize is the size of the short blob header (magic + uint32 length).
const BlobHeaderSize = 8

// BlobHeaderMagic64 marks the blob header a snapshot is written with: magic + uint64
// length. A record's length is capped below it (maxEntryLength).
const BlobHeaderMagic64 uint32 = 0xFFFFFFFE

// BlobHeaderSize64 is the size of the blob header a snapshot is written with.
const BlobHeaderSize64 = 12

// maxEntryLength is the longest record a length prefix may name: the two blob magics
// are not lengths.
const maxEntryLength = 0xFFFFFFFD

// isBlobMagic reports whether a length prefix is a blob header's magic.
func isBlobMagic(m uint32) bool { return m == BlobHeaderMagic || m == BlobHeaderMagic64 }

// blobHeaderSize is the size of the blob header whose magic is m.
func blobHeaderSize(m uint32) int64 {
	if m == BlobHeaderMagic64 {
		return BlobHeaderSize64
	}
	return BlobHeaderSize
}

// blobLength decodes the length field of a blob header whose magic is m, from the bytes
// after the magic. ok is false for a length no file can hold.
func blobLength(m uint32, field []byte) (n int64, ok bool) {
	if m == BlobHeaderMagic64 {
		u := binary.BigEndian.Uint64(field)
		if u > math.MaxInt64 {
			return 0, false
		}
		return int64(u), true
	}
	return int64(binary.BigEndian.Uint32(field)), true
}

// SnapshotWriter is a writer for creating snapshots in the inactive log.
// It implements io.WriteCloser and io.Seeker for use with snap.Builder.
// When closed, it writes the snapshot Entry metadata to the log.
//
// Log format for snapshots:
//
//	[blob header: 12 bytes]    - BlobHeaderMagic64 + uint64 blob length
//	[snapshot data: N bytes]   - binary event stream from snap.Builder
//	[entry: 4+M bytes]         - length prefix + Entry with SnapPos pointing to snapshot data
//
// The blob header allows the dlog iterator to skip over binary snapshot data.
type SnapshotWriter struct {
	dl          *DLog
	logFile     *DLogFile
	logFileID   LogFileID
	headerPos   int64 // where blob header is (for patching length on Close)
	startPos    int64 // where snapshot data starts (after blob header)
	endPos      int64 // where snapshot data ends (tracked on Write)
	commit      int64
	timestamp   string
	entryPos    int64 // set on Close
	closed      bool
	schemaEntry *SchemaEntry // optional schema change entry
	scopeID     *string      // optional scope ID for scoped snapshots
	snapPath    *string      // the path the snapshot is of; nil is the root
}

// ErrSnapshotInProgress is returned when attempting to start a snapshot
// while another snapshot is already running on the same log file.
var ErrSnapshotInProgress = fmt.Errorf("snapshot already in progress on this log file")

// NewSnapshotWriter creates a writer for building a snapshot in the inactive log.
// Returns ErrSnapshotInProgress if a snapshot is already running on the inactive log.
// The caller should create a snap.Builder with this writer, feed events to it, and
// close the builder, which closes this writer and so finalizes the Entry. A snapshot
// that fails before then is released with Abandon.
func (dl *DLog) NewSnapshotWriter(commit int64, timestamp string) (*SnapshotWriter, error) {
	dl.mu.Lock()
	activeLog := dl.activeLog

	// Determine inactive log
	var inactiveLog LogFileID
	var logFileObj *DLogFile
	if activeLog == LogFileA {
		inactiveLog = LogFileB
		logFileObj = dl.logB
	} else {
		inactiveLog = LogFileA
		logFileObj = dl.logA
	}

	// Try to acquire snapMu - don't block if snapshot already in progress
	if !logFileObj.snapMu.TryLock() {
		dl.mu.Unlock()
		return nil, ErrSnapshotInProgress
	}
	dl.mu.Unlock()

	// snapMu is now held - get current position and write blob header placeholder
	logFileObj.mu.Lock()
	headerPos := logFileObj.position

	// Write blob header placeholder: [magic marker][placeholder length]
	// The actual length will be patched in Close() once we know the blob size
	header := make([]byte, BlobHeaderSize64)
	binary.BigEndian.PutUint32(header[0:4], BlobHeaderMagic64)
	binary.BigEndian.PutUint64(header[4:12], 0) // placeholder, will be patched

	if _, err := logFileObj.file.WriteAt(header, headerPos); err != nil {
		logFileObj.mu.Unlock()
		logFileObj.snapMu.Unlock()
		return nil, fmt.Errorf("failed to write blob header: %w", err)
	}
	logFileObj.position = headerPos + BlobHeaderSize64
	startPos := logFileObj.position // snapshot data starts after header
	logFileObj.mu.Unlock()

	return &SnapshotWriter{
		dl:        dl,
		logFile:   logFileObj,
		logFileID: inactiveLog,
		headerPos: headerPos,
		startPos:  startPos,
		endPos:    startPos, // Initialize to start, will be updated by Write()
		commit:    commit,
		timestamp: timestamp,
	}, nil
}

// Write implements io.Writer
func (sw *SnapshotWriter) Write(p []byte) (n int, err error) {
	if sw.closed {
		return 0, fmt.Errorf("write to closed SnapshotWriter")
	}
	n, err = sw.logFile.file.WriteAt(p, sw.logFile.position)
	if err != nil {
		return n, err
	}
	sw.logFile.position += int64(n)
	// Track the highest position reached (end of snapshot data)
	if sw.logFile.position > sw.endPos {
		sw.endPos = sw.logFile.position
	}
	return n, nil
}

// Seek implements io.Seeker.
//
// The cursor is virtual: it moves logFile.position only, and never seeks the shared file
// descriptor. snap.Builder seeks backwards to patch its own header, and doing that on the
// real fd would relocate the append point for anyone else holding the file.
func (sw *SnapshotWriter) Seek(offset int64, whence int) (int64, error) {
	if sw.closed {
		return 0, fmt.Errorf("seek on closed SnapshotWriter")
	}
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = sw.logFile.position + offset
	case io.SeekEnd:
		stat, err := sw.logFile.file.Stat()
		if err != nil {
			return 0, fmt.Errorf("failed to stat log file: %w", err)
		}
		newPos = stat.Size() + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if newPos < 0 {
		return 0, fmt.Errorf("negative seek position %d", newPos)
	}
	sw.logFile.position = newPos
	// Don't update endPos on seek - only track actual writes
	return newPos, nil
}

// Close writes the snapshot Entry metadata and releases the snapshot lock.
// The Entry is written at the end of the snapshot data (endPos).
// Also patches the blob header with the actual blob length.
func (sw *SnapshotWriter) Close() error {
	if sw.closed {
		return nil
	}
	sw.closed = true
	defer sw.logFile.snapMu.Unlock()

	// Calculate blob length (snapshot data only, not including header or entry)
	blobLength := sw.endPos - sw.startPos

	// Patch the blob header with the actual length, in place at headerPos+4 (skipping the
	// magic marker). This in-place patch is why the log cannot be opened with O_APPEND.
	lenBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(lenBuf, uint64(blobLength))
	if _, err := sw.logFile.file.WriteAt(lenBuf, sw.headerPos+4); err != nil {
		return fmt.Errorf("failed to patch blob header length: %w", err)
	}

	// The Entry goes after the snapshot data; the builder may have left the cursor
	// somewhere earlier, so reset it to the high-water mark.
	sw.logFile.position = sw.endPos

	// Create snapshot entry pointing to the snapshot data (after blob header)
	snapPos := sw.startPos
	entry := &Entry{
		Commit:      sw.commit,
		Timestamp:   sw.timestamp,
		Patch:       nil,
		SnapPos:     &snapPos,
		TxSource:    nil,
		LastCommit:  nil,
		ScopeID:     sw.scopeID,
		SchemaEntry: sw.schemaEntry,
		SnapPath:    sw.snapPath,
	}

	// Binary event stream, matching AppendEntry — a snapshot's own Entry is a log record
	// like any other and is encoded the same way (see codec.go).
	entryBytes, err := encodeEntry(entry)
	if err != nil {
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	// The length must fit its prefix and not be one of the blob magics.
	if len(entryBytes) > maxEntryLength {
		return fmt.Errorf("entry too large: %d bytes (max %d)", len(entryBytes), maxEntryLength)
	}

	// Frame the entry in one buffer and write it in a single call, as AppendEntry does.
	entryPos := sw.logFile.position
	rec := make([]byte, 4+len(entryBytes))
	binary.BigEndian.PutUint32(rec[:4], uint32(len(entryBytes)))
	copy(rec[4:], entryBytes)

	if _, err := sw.logFile.file.WriteAt(rec, entryPos); err != nil {
		return fmt.Errorf("failed to write entry at %d: %w", entryPos, err)
	}

	sw.logFile.position = entryPos + int64(len(rec))
	sw.entryPos = entryPos

	return nil
}

// Abandon closes the SnapshotWriter without writing Entry metadata.
// Used when snapshot creation fails and we need to release the snapshot lock.
//
// It leaves the log walkable. The blob header is written with a placeholder length that
// only Close patches, so abandoning used to leave that placeholder — a header claiming
// length 0 — in the middle of the log, with the blob's data and every later append behind
// it. The frame walk cannot cross such a header, so iteration stopped there for the life
// of the file; a real log was found with 179 MB sitting behind one.
//
// Patching the header with what was actually written makes the region a well-formed blob
// that the walk skips like any other, just one no Entry refers to. Nothing points into it
// — a snapshot's index segment is added only after Close writes the Entry — so it is dead
// space until compaction drops it, not a hole.
//
// When nothing was written the header is all that exists, and rewinding the append point
// over it is both simpler and complete.
func (sw *SnapshotWriter) Abandon() {
	if sw.closed {
		return
	}
	sw.closed = true
	defer sw.logFile.snapMu.Unlock()

	blobLength := sw.endPos - sw.startPos
	if blobLength == 0 {
		sw.logFile.mu.Lock()
		sw.logFile.position = sw.headerPos
		sw.logFile.mu.Unlock()
		return
	}

	lenBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(lenBuf, uint64(blobLength))
	if _, err := sw.logFile.file.WriteAt(lenBuf, sw.headerPos+4); err != nil {
		// The header keeps its placeholder, so the walk will stop at it rather than
		// cross it. That is safe — nothing is deleted — but it is a hole, so say so.
		sw.logFile.logger.Error("failed to patch blob header of an abandoned snapshot; "+
			"the log has an uncrossable region from here",
			"path", sw.logFile.path, "headerPos", sw.headerPos, "error", err)
		return
	}
	sw.logFile.mu.Lock()
	sw.logFile.position = sw.endPos
	sw.logFile.mu.Unlock()
}

// EntryPosition returns the position of the Entry in the log (available after Close).
func (sw *SnapshotWriter) EntryPosition() int64 {
	return sw.entryPos
}

// LogFileID returns the log file ID where this snapshot was written.
func (sw *SnapshotWriter) LogFileID() LogFileID {
	return sw.logFileID
}

// SetSchemaEntry sets the schema entry for this snapshot.
// Must be called before Close().
func (sw *SnapshotWriter) SetSchemaEntry(schemaEntry *SchemaEntry) {
	sw.schemaEntry = schemaEntry
}

// SetScopeID sets the scope ID for this snapshot.
// Must be called before Close().
func (sw *SnapshotWriter) SetScopeID(scopeID *string) {
	sw.scopeID = scopeID
}

// SetPath says which path the snapshot is of. The root is the default and is recorded
// as nil, so a root snapshot's entry is the same record it always was. Must be called
// before Close().
func (sw *SnapshotWriter) SetPath(kp string) {
	if kp == "" {
		sw.snapPath = nil
		return
	}
	sw.snapPath = &kp
}
