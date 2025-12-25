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

		if entry.Patch != nil {
			if err := IndexPatch(idx, entry, string(logFile), pos, txSeq, entry.Patch, entry.ScopeID); err != nil {
				return fmt.Errorf("failed to index entry at commit %d: %w", entry.Commit, err)
			}
		}
	}

	return nil
}
