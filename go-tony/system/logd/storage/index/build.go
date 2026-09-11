package index

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// Unreadable says what of the logs the rebuild could not read: the regions the walk
// stepped over, or, when an error reading a log stopped the walk outright, where it
// stopped, everything after which the walk never reached.
type Unreadable struct {
	// LogFile and Position name where readable records stopped: the start of the first
	// region the walk could not read, or, when an error stopped the walk, the last record
	// it read.
	LogFile  string
	Position int64
	Err      error
	// Regions are what the walk stepped over -- each a range of one log file it could
	// not read, with readable records resumed at its end. Empty when the walk was
	// stopped outright by an error reading the file.
	Regions []Region
	// Dropped is how many index entries pointing into unreadable bytes were forgotten.
	Dropped int
}

// Region is a range of a log file the walk could not read.
type Region struct {
	LogFile  string
	From, To int64
}

func (u *Unreadable) String() string {
	if len(u.Regions) > 0 {
		return fmt.Sprintf("log %s: %d unreadable region(s), first at %d, stepped over (%d index entries dropped)",
			u.LogFile, len(u.Regions), u.Position, u.Dropped)
	}
	return fmt.Sprintf("log %s past position %d: %s (%d index entries dropped)",
		u.LogFile, u.Position, u.Err, u.Dropped)
}

// Build indexes every log entry with a commit past fromCommit. Its first result is non-nil
// when the walk stepped over a region it could not read, or was stopped by an error
// reading a log: the index holds every entry the walk read, and the caller decides what
// to tell an operator.
func Build(idx *Index, dlog *dlog.DLog, fromCommit int64) (*Unreadable, error) {
	return BuildWithLogger(idx, dlog, fromCommit, nil)
}

// BuildWithLogger is Build, logging what the walk could not read to logger when logger is
// non-nil.
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
			// An error reading a log stops the walk of the logs; it does not stop the
			// store. A record which will not deserialize does not come here: the walk
			// steps over it (dlog's resync, reported by Gaps below). Refusing to open
			// recovers nothing while making the store unavailable, which is how a
			// corrupt region in one log took a whole system down and kept it down
			// (t96b5ejqh12krprjghn0).
			//
			// It is said as loudly as a thing can be said short of refusing: an ERROR
			// naming the log and the offset, and Unreadable below, which the admin
			// listener reports for as long as the process runs.
			unreadable = &Unreadable{LogFile: lastFile, Position: lastPos, Err: err}
			if logger != nil {
				logger.Error("log will not read; indexing stops here and the store opens without what follows",
					"logFile", lastFile, "lastGoodPosition", lastPos, "error", err)
			}
			break
		}

		lastFile, lastPos = string(logFile), pos
		if entry.Commit <= fromCommit {
			continue
		}

		// Get current generation for this log file
		generation := dlog.GetGeneration(logFile)

		// The schema a schema commit sets, or the one a root snapshot was taken under:
		// either says which schema was in force from which commit (schema_history.go).
		if se := entry.SchemaEntry; se != nil && entry.ScopeID == nil {
			idx.NoteSchema(se.SetAt, se.Schema)
		}

		if entry.Patch != nil {
			// No schema is needed here: the entry holds the stored delta, the node the
			// live index was built from (commit_ops.go), and a keyed array in it is
			// already an object of its elements' names -- so the rebuild describes the
			// paths the live index did (TestKeyed_RebuiltIndexAgreesWithLive,
			// TestKeyed_RebuiltIndexUnderSchema).
			EachSegment(entry, string(logFile), pos, generation, idx.Add)
		} else if entry.SnapPos != nil {
			// A snapshot is indexed at the path it is of, and nowhere else.
			EachSegment(entry, string(logFile), pos, generation, idx.Add)
		}
	}

	// Regions the walk stepped over. Nothing behind them was dropped -- the walk resumed
	// at the next record it could read -- but a region the log cannot read is worth the
	// same noise as one that stopped it: an operator finding it weeks later in a log
	// file is finding it too late.
	if gaps := iter.Gaps(); len(gaps) > 0 && unreadable == nil {
		unreadable = &Unreadable{LogFile: string(gaps[0].LogFile), Position: gaps[0].From,
			Err: fmt.Errorf("%d unreadable region(s), first %s, stepped over", len(gaps), gaps[0])}
		for _, g := range gaps {
			unreadable.Regions = append(unreadable.Regions, Region{LogFile: string(g.LogFile), From: g.From, To: g.To})
			if logger != nil {
				logger.Error("log region will not read; indexing stepped over it", "region", g.String())
			}
		}
	}
	return unreadable, nil
}
