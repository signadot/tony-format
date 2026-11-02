package dlog

import "fmt"

// DropCache advises the kernel that the store is done with the first to bytes of log id
// at generation gen, and with all of the file the log's last compaction retired, so their
// page cache can go.
//
// A whole-log pass -- the snapshot a switch takes, the compaction after it, the index
// rebuild at open -- reads far more of the logs than reads at the head go on to touch,
// and what it read stayed cached, counted against the container though nothing wanted it
// again (whdn9vfbh12kryyxwxn0). A read of a dropped range is served from disk as it was
// before it was ever cached: only its speed depends on this. A generation the log is no
// longer at drops the retired file alone. Only Linux drops anything.
func (dl *DLog) DropCache(id LogFileID, gen, to int64) {
	lf, err := dl.fileFor(id)
	if err != nil {
		return
	}
	lf.mu.RLock()
	defer lf.mu.RUnlock()
	if to > 0 && gen == dl.GetGeneration(id) {
		if err := dropPages(lf.file, 0, to); err != nil {
			dl.logger.Warn("failed to drop log page cache", "logFile", id, "to", to, "error", err)
		}
	}
	if lf.retired != nil {
		if err := dropPages(lf.retired, 0, 0); err != nil {
			dl.logger.Warn("failed to drop retired log page cache", "logFile", id, "error", err)
		}
	}
}

// SnapshotStart answers where the snapshot whose entry is at position in log id begins:
// its blob header, which precedes its data and the entry.
func (dl *DLog) SnapshotStart(id LogFileID, position, gen int64) (int64, error) {
	entry, err := dl.ReadEntryAt(id, position, gen)
	if err != nil {
		return 0, err
	}
	if entry.SnapPos == nil {
		return 0, fmt.Errorf("entry at %s@%d is not a snapshot", id, position)
	}
	lf, err := dl.fileFor(id)
	if err != nil {
		return 0, err
	}
	lf.mu.RLock()
	defer lf.mu.RUnlock()
	if gen != dl.GetGeneration(id) {
		return 0, ErrCompactionInterrupted
	}
	hsize, err := snapBlobHeaderSize(lf, *entry.SnapPos)
	if err != nil {
		return 0, fmt.Errorf("blob header of the snapshot at %d: %w", *entry.SnapPos, err)
	}
	return *entry.SnapPos - hsize, nil
}

// SnapMark answers how far into the active log the newest snapshot's coverage reaches:
// what lies before it, a read at the head does not replay (see DLog.snapMark).
func (dl *DLog) SnapMark() int64 {
	return dl.snapMark.Load()
}
