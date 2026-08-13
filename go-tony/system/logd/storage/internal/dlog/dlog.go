package dlog

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"log/slog"
)

// ErrCompactionInterrupted is returned when a read fails because compaction
// changed the log file since the segment was indexed. Callers should re-lookup
// the segment from the index and retry.
var ErrCompactionInterrupted = errors.New("read interrupted by compaction")

// DLog manages the logA/logB double-buffered log files.
// Follows the double-buffering pattern where one log is active for writes
// and the other is inactive (being snapshotted or ready for compaction).
type DLog struct {
	baseDir   string       // Base directory for log files
	logA      *DLogFile    // First log file
	logB      *DLogFile    // Second log file
	activeLog LogFileID    // Which log is currently active ("A" or "B")
	mu        sync.RWMutex // Protects activeLog and file operations

	// Generation counters - incremented on compaction to detect stale reads
	generationA atomic.Int64
	generationB atomic.Int64

	// Reader refcounts - tracks active readers per log file for compaction safety
	readersA atomic.Int64
	readersB atomic.Int64

	// snapMark is how far into the ACTIVE log the last snapshot's coverage reaches:
	// the size that log had when it became active, which is the moment the snapshot
	// taken with it was written to the other one. Everything appended past it is
	// delta a reader has to replay on top of that snapshot, which is the only
	// quantity a size threshold can usefully bound (see DeltaBytesSinceSnapshot).
	// Persisted with the rest of the state, so a restart does not forget it.
	snapMark atomic.Int64

	// Metadata
	logger *slog.Logger // Logger for operations
}

// LogFileID identifies which log file (A or B)
type LogFileID string

const (
	LogFileA LogFileID = "A"
	LogFileB LogFileID = "B"
)

// DLogFile represents a single log file with its operations
type DLogFile struct {
	id       LogFileID    // "A" or "B"
	path     string       // Full path to log file (e.g., "logA" or "logB")
	file     *os.File     // Open file handle
	mu       sync.RWMutex // Protects file operations
	snapMu   sync.Mutex
	position int64 // Current write position (for appends)

	// Metadata
	logger *slog.Logger
}

// DLogIter provides sequential iteration over log entries using streaming reads.
// Uses streaming parsing to avoid loading entire entries into memory.
// Iterates over both logA and logB, switching between them based on commit order.
type DLogIter struct {
	dlog  *DLog
	iterA *singleFileIter
	iterB *singleFileIter
	nextA *Entry
	nextB *Entry
	posA  int64
	posB  int64
	done  bool
}

type singleFileIter struct {
	logFile  *DLogFile
	position int64
	done     bool
	fileSize int64
}

// NewDLog creates a new double-buffered log manager.
// Initializes both logA and logB files, determines active log from state.
// Defaults to LogA as active if no state exists.
func NewDLog(baseDir string, logger *slog.Logger) (*DLog, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	logAPath := filepath.Join(baseDir, "logA")
	logBPath := filepath.Join(baseDir, "logB")

	// Recover any compaction that was interrupted before it finished (crash between the two
	// renames in swapLogFile, or before the post-swap index was made durable). This restores
	// the log from its `.old` undo copy and discards any `.compact.tmp`, so the file matches
	// the last durable state. Must run BEFORE opening the files. See issue 656g8yt5.
	recoverCompactionArtifacts(logAPath, logger)
	recoverCompactionArtifacts(logBPath, logger)

	// Initialize logA
	logA, err := newDLogFile(LogFileA, logAPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logA: %w", err)
	}

	// Initialize logB
	logB, err := newDLogFile(LogFileB, logBPath, logger)
	if err != nil {
		logA.Close() // Clean up on error
		return nil, fmt.Errorf("failed to initialize logB: %w", err)
	}

	// Restore active log AND the per-file generation counters from state. Persisting the
	// generation is what lets a restart detect a stale index after compaction (the generation
	// is the staleness token used by ReadEntryAt); without it, generation reset to 0 on every
	// restart and reads of compacted data silently returned the wrong bytes. Format:
	// "<active> <genA> <genB> <snapMark>"; a bare "A"/"B" (legacy) means generations 0, and a
	// state file written before snapMark existed means 0 — the whole active log counts as
	// delta, which errs toward snapshotting sooner.
	activeLog := LogFileA
	var genA, genB, snapMark int64
	statePath := filepath.Join(baseDir, "dlog.state")
	if stateData, err := os.ReadFile(statePath); err == nil && len(stateData) > 0 {
		var active string
		if n, _ := fmt.Sscanf(string(stateData), "%s %d %d %d", &active, &genA, &genB, &snapMark); n >= 1 {
			if active == "B" {
				activeLog = LogFileB
			}
		}
	}

	dl := &DLog{
		baseDir:   baseDir,
		logA:      logA,
		logB:      logB,
		activeLog: activeLog,
		logger:    logger,
	}
	dl.generationA.Store(genA)
	dl.generationB.Store(genB)
	dl.snapMark.Store(snapMark)

	return dl, nil
}

// recoverCompactionArtifacts rolls back an interrupted compaction of one log file. `.old` is
// the undo copy created by swapLogFile before it replaces the file; its presence means the
// compaction did not finish durably, so restore it (this overwrites a partially-swapped or
// missing file with the pre-compaction original). A leftover `.compact.tmp` is discarded. The
// compaction simply re-runs later. Best-effort: failures are logged, not fatal.
func recoverCompactionArtifacts(path string, logger *slog.Logger) {
	oldPath := path + ".old"
	tmpPath := path + ".compact.tmp"
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Rename(oldPath, path); err != nil {
			logger.Error("compaction recovery: failed to restore log from .old", "path", path, "error", err)
		} else {
			logger.Warn("compaction recovery: restored log from an interrupted compaction", "path", path)
		}
	}
	if _, err := os.Stat(tmpPath); err == nil {
		if err := os.Remove(tmpPath); err != nil {
			logger.Warn("compaction recovery: failed to remove stale temp", "path", tmpPath, "error", err)
		}
	}
}

// writeState persists the active log and both generation counters to dlog.state, durably
// (fsync of the file and its directory), so a compaction's generation bump and the active-log
// choice survive a crash. Written via a temp file + rename so a crash never leaves a
// half-written state file.
func (dl *DLog) writeState() error {
	dl.mu.RLock()
	active := dl.activeLog
	dl.mu.RUnlock()
	line := fmt.Sprintf("%s %d %d %d", active, dl.generationA.Load(), dl.generationB.Load(), dl.snapMark.Load())

	statePath := filepath.Join(dl.baseDir, "dlog.state")
	tmpPath := statePath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(line)); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, statePath); err != nil {
		return err
	}
	return fsyncDir(dl.baseDir)
}

// fsyncDir fsyncs a directory so a rename/create within it is durable.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// newDLogFile creates and opens a log file for appending.
//
// All writes go through WriteAt (pwrite) at an explicit offset, and all reads through
// ReadAt, so the OS file offset is never used or advanced. DLogFile.position is the sole
// authority for where the next record goes. This is deliberate: O_APPEND cannot work here
// because the snapshot writer patches a blob header in place (see SnapshotWriter.Close),
// and an offset-based append gives two sources of truth for the write frontier that
// nothing keeps in sync — a failed append, or a reopened handle, silently desynchronizes
// them and every later entry reports a position that does not point at its own header.
func newDLogFile(id LogFileID, path string, logger *slog.Logger) (*DLogFile, error) {
	// Open file in read-write mode (create if doesn't exist)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", path, err)
	}

	// Get current file size (upper bound for the append position)
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat log file %q: %w", path, err)
	}

	// Records are not fsynced on append, so the tail may be torn: a length prefix whose
	// payload never reached disk, or a partial prefix. Adopting stat.Size() as the append
	// point would write the next record after that stump and put every frame boundary from
	// there on at the wrong offset. Find the end of the last complete frame instead.
	end, tornTail, err := scanFrames(file, stat.Size())
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to scan log file %q: %w", path, err)
	}
	// Opening a log never deletes anything. The scan decides where the next record goes,
	// not what survives, so a scan that is wrong costs a failed read rather than bytes.
	//
	// A torn tail needs no truncation to be dealt with: appends are WriteAt at position,
	// so setting position to the last complete frame means the next record simply
	// overwrites the stump. Truncation only ever bought a clean bound for iteration, and
	// the iterator takes that bound from position instead (see Iterator).
	position := stat.Size()
	switch {
	case tornTail:
		logger.Warn("log file ends in an incomplete record; it will be overwritten",
			"path", path, "lastGoodEnd", end, "size", stat.Size(),
			"incompleteBytes", stat.Size()-end)
		position = end
	case end < stat.Size():
		// The walk stopped on something it cannot cross, with the file continuing past
		// it — an abandoned snapshot leaves an unpatched blob header mid-file with live
		// entries behind it. Append at the real end of file so nothing already written
		// is overwritten.
		logger.Warn("log file has a region the frame walk cannot cross; appending past it",
			"path", path, "stoppedAt", end, "size", stat.Size(),
			"bytesBeyond", stat.Size()-end)
	}

	return &DLogFile{
		id:       id,
		path:     path,
		file:     file,
		position: position,
		logger:   logger,
	}, nil
}

// scanFrames walks the record framing from the start of the file and returns the offset
// just past the last complete frame, plus whether everything from there to size is a
// PROVABLY incomplete tail. It mirrors the framing that singleFileIter.next reads: a
// 4-byte big-endian length prefix followed by that many payload bytes, or a
// BlobHeaderMagic marker followed by a 4-byte blob length and that many blob bytes.
//
// The bool is the whole safety story. tornTail is true only when the walk reaches a frame
// that runs past the end of the file — that frame was interrupted mid-write, nothing can
// follow it, and discarding it costs at most that one record. Every other stop means the
// walk hit something it cannot interpret while the file continues past it, which is not a
// tail and must never be discarded: an abandoned snapshot (SnapshotWriter.Abandon, or a
// crash before Close patches the header) leaves a blob header holding its placeholder
// length of 0, with the blob's data and every entry appended afterwards sitting behind it.
// Treating that as a tail truncates the entire remainder of the log — on a real 185 MB
// verse log it proposed discarding 179 MB.
//
// It does not validate payload contents, so a frame boundary landing on plausible-looking
// garbage is still accepted; per-record checksums are what would make this decisive.
func scanFrames(file *os.File, size int64) (end int64, tornTail bool, err error) {
	hdr := make([]byte, 4)
	pos := int64(0)
	for pos < size {
		if pos+4 > size {
			return pos, true, nil // partial length prefix: interrupted mid-write
		}
		if _, err := file.ReadAt(hdr, pos); err != nil {
			if err == io.EOF {
				return pos, true, nil
			}
			return 0, false, fmt.Errorf("read length prefix at %d: %w", pos, err)
		}
		lengthOrMagic := binary.BigEndian.Uint32(hdr)

		if lengthOrMagic == BlobHeaderMagic {
			if pos+BlobHeaderSize > size {
				return pos, true, nil // partial blob header
			}
			if _, err := file.ReadAt(hdr, pos+4); err != nil {
				if err == io.EOF {
					return pos, true, nil
				}
				return 0, false, fmt.Errorf("read blob length at %d: %w", pos, err)
			}
			blobLen := int64(binary.BigEndian.Uint32(hdr))
			if blobLen == 0 {
				// Unpatched placeholder: an abandoned or interrupted snapshot. The
				// blob's extent is unknowable from here, so the walk stops — but the
				// data beyond it is real and is not ours to drop.
				return pos, false, nil
			}
			blobEnd := pos + BlobHeaderSize + blobLen
			if blobEnd > size {
				return pos, true, nil // blob data cut short
			}
			pos = blobEnd
			continue
		}

		// A zero length is never a real entry. At the very end of the file it is
		// unwritten space; with data behind it, it is something this walk cannot read,
		// and the difference is not decidable here — so never call it a tail.
		if lengthOrMagic == 0 {
			return pos, false, nil
		}
		frameEnd := pos + 4 + int64(lengthOrMagic)
		if frameEnd > size {
			return pos, true, nil // payload cut short
		}
		pos = frameEnd
	}
	return pos, false, nil
}

// AppendEntry appends an Entry to the active log file.
// The Entry.Commit is provided by the caller (from seq.Seq.NextCommit()).
// Returns the log position and which log file (A or B) it was written to.
// Does NOT automatically switch active log - that's handled by the caller
// when compaction boundaries are reached.
func (dl *DLog) AppendEntry(entry *Entry) (logPosition int64, logFile LogFileID, err error) {
	dl.mu.Lock()
	activeLog := dl.activeLog
	dl.mu.Unlock()

	var logFileObj *DLogFile
	if activeLog == LogFileA {
		logFileObj = dl.logA
	} else {
		logFileObj = dl.logB
	}

	position, err := logFileObj.AppendEntry(entry)
	if err != nil {
		return 0, "", fmt.Errorf("failed to append entry to %s: %w", activeLog, err)
	}

	return position, activeLog, nil
}

// ReadEntryAt reads an Entry from the specified log file at the given position.
// logFile must be "A" or "B".
// expectedGeneration should match the generation when the segment was indexed.
// Returns ErrCompactionInterrupted if generation doesn't match (caller should re-lookup and retry).
// Automatically tracks reader refcount for compaction safety.
func (dl *DLog) ReadEntryAt(logFile LogFileID, position int64, expectedGeneration int64) (*Entry, error) {
	// Check generation before reading
	currentGen := dl.GetGeneration(logFile)
	if currentGen != expectedGeneration {
		return nil, ErrCompactionInterrupted
	}

	var logFileObj *DLogFile
	switch logFile {
	case LogFileA:
		logFileObj = dl.logA
	case LogFileB:
		logFileObj = dl.logB
	default:
		return nil, fmt.Errorf("invalid log file ID: %q (must be A or B)", logFile)
	}

	dl.AcquireReader(logFile)
	defer dl.ReleaseReader(logFile)

	return logFileObj.ReadEntryAt(position)
}

// OpenReaderAt opens a reader at the specified position in the log file.
// This is used to read inline snapshot data stored in the log.
// All seeks in the returned reader are relative to position (position becomes offset 0).
// The returned reader must be closed when done.
// logFile must be "A" or "B".
// expectedGeneration should match the generation when the segment was indexed.
// Returns ErrCompactionInterrupted if generation doesn't match.
// Automatically tracks reader refcount for compaction safety - refcount is released on Close.
func (dl *DLog) OpenReaderAt(logFile LogFileID, position int64, expectedGeneration int64) (io.ReadSeekCloser, error) {
	// Check generation before opening
	currentGen := dl.GetGeneration(logFile)
	if currentGen != expectedGeneration {
		return nil, ErrCompactionInterrupted
	}

	var logFileObj *DLogFile
	switch logFile {
	case LogFileA:
		logFileObj = dl.logA
	case LogFileB:
		logFileObj = dl.logB
	default:
		return nil, fmt.Errorf("invalid log file ID: %q (must be A or B)", logFile)
	}

	dl.AcquireReader(logFile)

	reader, err := logFileObj.OpenReaderAt(position)
	if err != nil {
		dl.ReleaseReader(logFile)
		return nil, err
	}

	return &refcountedReader{
		ReadSeekCloser: reader,
		dlog:           dl,
		logFile:        logFile,
	}, nil
}

// refcountedReader wraps a reader to release refcount on Close.
type refcountedReader struct {
	io.ReadSeekCloser
	dlog    *DLog
	logFile LogFileID
	closed  bool
}

func (r *refcountedReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	err := r.ReadSeekCloser.Close()
	r.dlog.ReleaseReader(r.logFile)
	return err
}

// GetActiveLog returns the currently active log file ID.
func (dl *DLog) GetActiveLog() LogFileID {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return dl.activeLog
}

// GetInactiveLog returns the currently inactive log file ID.
func (dl *DLog) GetInactiveLog() LogFileID {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	if dl.activeLog == LogFileA {
		return LogFileB
	}
	return LogFileA
}

// GetGeneration returns the current generation for a log file.
// Generation is incremented on compaction to detect stale reads.
func (dl *DLog) GetGeneration(id LogFileID) int64 {
	if id == LogFileA {
		return dl.generationA.Load()
	}
	return dl.generationB.Load()
}

// IncrementGeneration increments the generation counter for a log file.
// Called after compaction to invalidate stale segment references.
func (dl *DLog) IncrementGeneration(id LogFileID) {
	if id == LogFileA {
		dl.generationA.Add(1)
	} else {
		dl.generationB.Add(1)
	}
	// Persist the bump durably: the generation is the token a restart uses to detect a stale
	// index after compaction, so it must survive a crash (issue 656g8yt5).
	if err := dl.writeState(); err != nil {
		dl.logger.Warn("failed to persist generation state", "error", err)
	}
}

// ActiveLogSize returns the current size of the active log file.
func (dl *DLog) ActiveLogSize() (int64, error) {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	var logFile *DLogFile
	if dl.activeLog == LogFileA {
		logFile = dl.logA
	} else {
		logFile = dl.logB
	}
	return logFile.Size()
}

// DeltaBytesSinceSnapshot reports how much of the active log a read has to replay on
// top of the newest snapshot: everything appended since this log became active.
//
// This, and not ActiveLogSize, is what a size-based snapshot policy has to threshold.
// A switch does not empty the log it switches INTO — that log still holds the records
// from its previous turn, and truncating them is compaction's decision, not the
// switch's — so the active log's size never comes back down. A policy reading it
// crosses its threshold once and then never uncrosses it: it snapshots on every
// commit from then on, forever, which is how a 100 KB store grew 4 MB of logs in 400
// writes (issue ps8kfs9dh12kr777fnn0).
//
// A mark ahead of the file means compaction rewrote it shorter since; count the whole
// file as delta rather than none, since erring toward a snapshot costs one snapshot
// and the switch that takes it re-marks the log.
func (dl *DLog) DeltaBytesSinceSnapshot() (int64, error) {
	size, err := dl.ActiveLogSize()
	if err != nil {
		return 0, err
	}
	mark := dl.snapMark.Load()
	if mark > size {
		return size, nil
	}
	return size - mark, nil
}

// Sync forces the given log file's appended records to stable storage.
// Appends go to the page cache (see DLogFile.AppendEntry); until this returns, a
// machine crash can lose records the caller has already been told were written.
func (dl *DLog) Sync(id LogFileID) error {
	switch id {
	case LogFileA:
		return dl.logA.Sync()
	case LogFileB:
		return dl.logB.Sync()
	default:
		return fmt.Errorf("unknown log file %q", id)
	}
}

// SyncAll forces both log files to stable storage. Used on a clean shutdown, and
// available to a caller that wants a flush point without knowing which log is active.
func (dl *DLog) SyncAll() error {
	var errs error
	if err := dl.logA.Sync(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to sync logA: %w", err))
	}
	if err := dl.logB.Sync(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to sync logB: %w", err))
	}
	return errs
}

// SwitchActive switches the active log (A ↔ B).
// Blocks if a snapshot is in progress on the inactive log (which is about to become active).
// Called by the caller when compaction boundaries are reached.
func (dl *DLog) SwitchActive() error {
	dl.mu.Lock()

	// Determine inactive log and acquire its snapMu
	// This blocks if a snapshot is running on that log
	var inactiveLog *DLogFile
	if dl.activeLog == LogFileA {
		inactiveLog = dl.logB
	} else {
		inactiveLog = dl.logA
	}

	// Block until any snapshot on inactive log completes
	inactiveLog.snapMu.Lock()

	// Switch active log
	var newActive LogFileID
	if dl.activeLog == LogFileA {
		newActive = LogFileB
	} else {
		newActive = LogFileA
	}
	dl.activeLog = newActive

	// Mark where this log stands as it becomes active. The snapshot about to be
	// written covers everything up to here, so only what is appended past this point
	// is delta a reader must replay (see DeltaBytesSinceSnapshot). A size we cannot
	// read is recorded as 0, which counts the whole log as delta — snapshotting
	// sooner than needed rather than not at all.
	if size, err := inactiveLog.Size(); err == nil {
		dl.snapMark.Store(size)
	} else {
		dl.logger.Warn("failed to size the newly active log; snapshot threshold will count it whole",
			"log", newActive, "error", err)
		dl.snapMark.Store(0)
	}

	// Release snapMu - the old inactive is now active and can receive writes
	inactiveLog.snapMu.Unlock()
	dl.mu.Unlock()

	// Persist state (active log + generations) durably.
	if err := dl.writeState(); err != nil {
		// Log error but don't fail - state can be recovered
		dl.logger.Warn("failed to persist active log state", "error", err)
	}

	return nil
}

// Iterator creates an iterator for reading entries from both log files in commit order.
// Starts at position 0 for both files.
// Note: Currently uses non-streaming reads. Streaming support can be added later.
func (dl *DLog) Iterator() (*DLogIter, error) {
	// Bound by the append frontier, not the file size. position is the end of the last
	// complete record: on open it is where the frame scan stopped, and every append moves
	// it by exactly what was written. An incomplete tail therefore falls outside the
	// iteration without having to be deleted from the file first — which is what let
	// opening a log stop truncating.
	sizeA := dl.logA.Position()
	sizeB := dl.logB.Position()

	iterA := &singleFileIter{
		logFile:  dl.logA,
		position: 0,
		done:     sizeA == 0,
		fileSize: sizeA,
	}
	iterB := &singleFileIter{
		logFile:  dl.logB,
		position: 0,
		done:     sizeB == 0,
		fileSize: sizeB,
	}

	it := &DLogIter{
		dlog:  dl,
		iterA: iterA,
		iterB: iterB,
	}

	// Peek at first entries from both files
	if !iterA.done {
		entry, pos, err := iterA.next()
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read first entry from logA: %w", err)
		}
		if err == nil {
			it.nextA = entry
			it.posA = pos
		}
	}
	if !iterB.done {
		entry, pos, err := iterB.next()
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read first entry from logB: %w", err)
		}
		if err == nil {
			it.nextB = entry
			it.posB = pos
		}
	}

	if it.nextA == nil && it.nextB == nil {
		it.done = true
	}

	return it, nil
}

// LogFilePath returns the file path for the specified log file ID.
func (dl *DLog) LogFilePath(id LogFileID) string {
	switch id {
	case LogFileA:
		return dl.logA.path
	case LogFileB:
		return dl.logB.path
	default:
		return ""
	}
}

// Close closes both log files, flushing them first: a clean shutdown is the one
// point where durability costs nothing, whatever the per-commit durability mode.
func (dl *DLog) Close() error {
	var errs error

	if err := dl.SyncAll(); err != nil {
		errs = errors.Join(errs, err)
	}

	if err := dl.logA.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to close logA: %w", err))
	}

	if err := dl.logB.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to close logB: %w", err))
	}
	return errs
}

// AppendEntry appends an Entry to this log file.
// Format: [4 bytes: uint32 length (big-endian)][entry data in Tony wire format]
// Returns the byte position where the entry was written.
func (dlf *DLogFile) AppendEntry(entry *Entry) (position int64, err error) {
	dlf.mu.Lock()
	defer dlf.mu.Unlock()

	// Serialize entry to the binary event stream (see codec.go)
	entryBytes, err := encodeEntry(entry)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Check length fits in uint32
	if len(entryBytes) > 0xFFFFFFFF {
		return 0, fmt.Errorf("entry too large: %d bytes (max %d)", len(entryBytes), 0xFFFFFFFF)
	}

	// Frame the record in one buffer and issue a single write, so there is no window in
	// which a length prefix exists on disk without its payload. (This is not crash
	// atomicity — a single write can still tear — which is why scanFrames runs on open.)
	rec := make([]byte, 4+len(entryBytes))
	binary.BigEndian.PutUint32(rec[:4], uint32(len(entryBytes)))
	copy(rec[4:], entryBytes)

	// Get current position before writing
	currentPos := dlf.position

	// WriteAt neither reads nor advances the file offset, so position stays the only
	// authority. On failure position is left untouched and the next append rewrites these
	// bytes, rather than appending after a stump the index would never point at.
	if _, err := dlf.file.WriteAt(rec, currentPos); err != nil {
		return 0, fmt.Errorf("failed to write entry at %d: %w", currentPos, err)
	}

	// Update position
	dlf.position = currentPos + int64(len(rec))

	return currentPos, nil
}

// ReadEntryAt reads an Entry from the specified byte position.
// Reads length prefix, then entry data, deserializes to Entry.
func (dlf *DLogFile) ReadEntryAt(position int64) (*Entry, error) {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()

	// Read length prefix (4 bytes, big-endian uint32)
	lengthBytes := make([]byte, 4)
	if _, err := dlf.file.ReadAt(lengthBytes, position); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("position %d: reached EOF while reading length prefix", position)
		}
		return nil, fmt.Errorf("failed to read length prefix at position %d: %w", position, err)
	}

	length := int64(binary.BigEndian.Uint32(lengthBytes))

	// Read entry data
	entryBytes := make([]byte, length)
	if _, err := dlf.file.ReadAt(entryBytes, position+4); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("position %d: reached EOF while reading entry data (expected %d bytes)", position+4, length)
		}
		return nil, fmt.Errorf("failed to read entry data at position %d: %w", position+4, err)
	}

	// Deserialize entry (binary, or legacy tony text — see codec.go)
	entry, err := decodeEntry(entryBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize entry at position %d: %w", position, err)
	}

	return entry, nil
}

// Size returns the current size of the log file.
func (dlf *DLogFile) Size() (int64, error) {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()

	stat, err := dlf.file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat log file: %w", err)
	}

	return stat.Size(), nil
}

// Position returns the current write position (for appends).
func (dlf *DLogFile) Position() int64 {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()
	return dlf.position
}

// Sync forces this log file's contents to stable storage.
//
// RLock, not Lock: the only field read is the handle (Close nils it under Lock), and
// fsync is safe alongside a concurrent pwrite. A caller syncing its own append has
// already returned from AppendEntry, so the bytes it cares about are in the file;
// whether a later concurrent append also lands in this flush does not matter.
func (dlf *DLogFile) Sync() error {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()

	if dlf.file == nil {
		return nil // closed
	}
	if err := dlf.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync log file %q: %w", dlf.path, err)
	}
	return nil
}

// Close closes the log file.
func (dlf *DLogFile) Close() error {
	dlf.mu.Lock()
	defer dlf.mu.Unlock()

	if dlf.file == nil {
		return nil // Already closed
	}

	if err := dlf.file.Close(); err != nil {
		return fmt.Errorf("failed to close log file %q: %w", dlf.path, err)
	}

	dlf.file = nil
	return nil
}

// OpenReaderAt returns a reader scoped to the section starting at position.
// All seeks in the returned reader are relative to position (position becomes offset 0).
// This is used to read inline snapshot data.
// Uses the existing file handle with ReadAt (pread) - no new file handle opened.
// The returned reader's Close() is a no-op since it doesn't own the file handle.
func (dlf *DLogFile) OpenReaderAt(position int64) (io.ReadSeekCloser, error) {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()

	// Get file size to determine section size
	stat, err := dlf.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat log file: %w", err)
	}

	// Create a SectionReader from position to end of file
	// This makes all seeks relative to position (position becomes offset 0)
	// SectionReader uses ReadAt (pread) - concurrent-safe, no file pointer movement
	sectionSize := stat.Size() - position
	section := io.NewSectionReader(dlf.file, position, sectionSize)

	// Wrap in a no-op closer since we don't own the file handle
	return &sectionReadCloser{section}, nil
}

// sectionReadCloser wraps an io.SectionReader with a no-op Close method.
// The underlying file handle is owned by DLogFile, not by this reader.
type sectionReadCloser struct {
	*io.SectionReader
}

func (src *sectionReadCloser) Close() error {
	return nil // no-op - we don't own the file handle
}

func (it *singleFileIter) next() (*Entry, int64, error) {
	if it.done {
		return nil, it.position, io.EOF
	}

	if it.position >= it.fileSize {
		it.done = true
		return nil, it.position, io.EOF
	}

	lengthBytes := make([]byte, 4)
	it.logFile.mu.RLock()
	_, err := it.logFile.file.ReadAt(lengthBytes, it.position)
	it.logFile.mu.RUnlock()
	if err != nil {
		if err == io.EOF {
			it.done = true
			return nil, it.position, io.EOF
		}
		return nil, it.position, fmt.Errorf("failed to read length prefix: %w", err)
	}

	lengthOrMagic := binary.BigEndian.Uint32(lengthBytes)

	// Check for blob header magic marker (snapshot data)
	if lengthOrMagic == BlobHeaderMagic {
		// Read blob length (next 4 bytes after magic)
		blobLenBytes := make([]byte, 4)
		it.logFile.mu.RLock()
		_, err := it.logFile.file.ReadAt(blobLenBytes, it.position+4)
		it.logFile.mu.RUnlock()
		if err != nil {
			if err == io.EOF {
				it.done = true
				return nil, it.position, io.EOF
			}
			return nil, it.position, fmt.Errorf("failed to read blob length: %w", err)
		}
		blobLength := int64(binary.BigEndian.Uint32(blobLenBytes))

		// Skip blob header (8 bytes) + blob data
		it.position += BlobHeaderSize + blobLength
		if it.position >= it.fileSize {
			it.done = true
			return nil, it.position, io.EOF
		}

		// Recursively call next to read the entry after the blob
		return it.next()
	}

	entryLength := int64(lengthOrMagic)
	oldPosition := it.position

	entryBytes := make([]byte, entryLength)
	it.logFile.mu.RLock()
	_, err = it.logFile.file.ReadAt(entryBytes, it.position+4)
	it.logFile.mu.RUnlock()
	if err != nil {
		if err == io.EOF {
			it.done = true
			return nil, it.position, io.EOF
		}
		return nil, it.position, fmt.Errorf("failed to read entry data: %w", err)
	}

	entry, err := decodeEntry(entryBytes)
	if err != nil {
		return nil, it.position, fmt.Errorf("failed to deserialize entry: %w", err)
	}

	it.position = oldPosition + 4 + entryLength
	if it.position >= it.fileSize {
		it.done = true
	}

	return entry, oldPosition, nil
}

// Next reads and returns the next entry from both log files in commit order.
// Returns the entry, its log file ID and position, and any error.
// Returns io.EOF when both files are exhausted.
func (it *DLogIter) Next() (*Entry, LogFileID, int64, error) {
	if it.done {
		return nil, "", 0, io.EOF
	}

	// Refresh next entries if needed
	if it.nextA == nil && !it.iterA.done {
		entry, pos, err := it.iterA.next()
		if err != nil && err != io.EOF {
			return nil, "", 0, fmt.Errorf("failed to read from logA: %w", err)
		}
		if err == nil {
			it.nextA = entry
			it.posA = pos
		}
	}
	if it.nextB == nil && !it.iterB.done {
		entry, pos, err := it.iterB.next()
		if err != nil && err != io.EOF {
			return nil, "", 0, fmt.Errorf("failed to read from logB: %w", err)
		}
		if err == nil {
			it.nextB = entry
			it.posB = pos
		}
	}

	// Choose entry with lower commit number
	var entry *Entry
	var logFile LogFileID
	var pos int64

	if it.nextA == nil && it.nextB == nil {
		it.done = true
		return nil, "", 0, io.EOF
	} else if it.nextA == nil {
		entry = it.nextB
		logFile = LogFileB
		pos = it.posB
		it.nextB = nil
	} else if it.nextB == nil {
		entry = it.nextA
		logFile = LogFileA
		pos = it.posA
		it.nextA = nil
	} else if it.nextA.Commit <= it.nextB.Commit {
		entry = it.nextA
		logFile = LogFileA
		pos = it.posA
		it.nextA = nil
	} else {
		entry = it.nextB
		logFile = LogFileB
		pos = it.posB
		it.nextB = nil
	}

	return entry, logFile, pos, nil
}

// Done returns true if iterator has reached end of both files.
func (it *DLogIter) Done() bool {
	return it.done
}
