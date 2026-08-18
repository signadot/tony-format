package index

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// Unreadable names a record the rebuild could not deserialize, and everything after it
// in that log, which the walk therefore never reached.
type Unreadable struct {
	// LogFile and Position name the last record read SUCCESSFULLY in the log which
	// then failed: the walk cannot say where a bad frame ends, only where the last
	// good one did, and everything past that point in that log is unreachable.
	LogFile  string
	Position int64
	Err      error
	// Dropped is how many index entries beyond it were forgotten as a result.
	Dropped int
}

func (u *Unreadable) String() string {
	return fmt.Sprintf("log %s past position %d: %s (%d index entries dropped)",
		u.LogFile, u.Position, u.Err, u.Dropped)
}

// Build indexes the log from fromCommit. Its second result is non-nil when a record
// would not deserialize: the index holds everything up to that point, and the caller
// decides what to tell an operator.
func Build(idx *Index, dlog *dlog.DLog, fromCommit int64) (*Unreadable, error) {
	return BuildWithLogger(idx, dlog, fromCommit, nil)
}

func BuildWithLogger(idx *Index, dlog *dlog.DLog, fromCommit int64, logger *slog.Logger) (*Unreadable, error) {
	var unreadable *Unreadable
	// The last record read successfully, which is the only boundary the walk knows:
	// a frame it cannot parse has no end.
	var lastFile string
	var lastPos int64
	iter, err := dlog.Iterator()
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}

	for {
		entry, logFile, pos, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			// A record which will not deserialize stops the walk of the logs; it does
			// not stop the store. Framing past a bad frame cannot be trusted, so what
			// lies beyond is unreachable either way -- and refusing to open recovers
			// none of it while making the store unavailable, which is how a corrupt
			// region in one log took a whole system down and kept it down
			// (t96b5ejqh12krprjghn0).
			//
			// It is said as loudly as a thing can be said short of refusing: an ERROR
			// naming the log and the offset, and Unreadable below, which the admin
			// listener reports for as long as the process runs.
			unreadable = &Unreadable{LogFile: lastFile, Position: lastPos, Err: err}
			if logger != nil {
				logger.Error("log record will not deserialize; indexing stops here and the store opens without what follows",
					"logFile", lastFile, "lastGoodPosition", lastPos, "error", err)
			}
			break
		}

		lastFile, lastPos = string(logFile), pos
		if entry.Commit <= fromCommit {
			continue
		}

		txSeq := int64(0)
		if entry.TxSource != nil {
			txSeq = entry.TxSource.TxID
		}

		// Get current generation for this log file
		generation := dlog.GetGeneration(logFile)

		if entry.Patch != nil {
			// Schema is nil here, so keyed arrays are recognised only by the !key tag a
			// patch carries. That does NOT reproduce a live index built under a schema:
			// the schema route (AutoIDFields) keys arrays whose patches carry no tag, so
			// the same log rebuilds to items[0] where the live index held items("<id>").
			//
			// Harmless as things stand, and measured: path-level entries have one
			// consumer, ReadPatchesInRange, and LookupRange collects a node's own
			// segments before descending — so a lookup at any path already returns every
			// entry's root copy and both shapes answer a replay identically
			// (TestKeyed_RebuildDivergenceImpact). It stops being harmless for anything
			// that addresses BY the keyed path, which is what a scope overlay does; see
			// docs/scope_overlay_plan.md P1.
			if err := IndexPatch(idx, entry, string(logFile), pos, txSeq, generation, entry.Patch, nil, entry.ScopeID); err != nil {
				return nil, fmt.Errorf("failed to index entry at commit %d: %w", entry.Commit, err)
			}
		} else if entry.SnapPos != nil {
			// Snapshot entry - add to index for state reconstruction
			// Snapshots have StartCommit == EndCommit
			seg := &LogSegment{
				StartCommit:       entry.Commit,
				EndCommit:         entry.Commit,
				StartTx:           0,
				EndTx:             0,
				KindedPath:        "",
				LogFile:           string(logFile),
				LogPosition:       pos,
				LogFileGeneration: generation,
				ScopeID:           entry.ScopeID,
			}
			idx.Add(seg)
		}
	}

	return unreadable, nil
}
