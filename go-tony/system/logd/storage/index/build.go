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
			// Schema is nil here - we rely on !key tags stored in the patches
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
