package index

import (
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

func Build(idx *Index, dlog *dlog.DLog, fromCommit int64) error {
	iter, err := dlog.Iterator()
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}

	for {
		entry, logFile, pos, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read entry: %w", err)
		}

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
				return fmt.Errorf("failed to index entry at commit %d: %w", entry.Commit, err)
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

	return nil
}
