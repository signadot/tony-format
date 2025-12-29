package storage

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

type commitOps struct {
	s *Storage
}

func (c *commitOps) ReadStateAt(kpath string, commit int64, scopeID *string) (*ir.Node, error) {
	return c.s.ReadStateAt(kpath, commit, scopeID)
}

func (c *commitOps) GetCurrentCommit() (int64, error) {
	return c.s.GetCurrentCommit()
}

func (c *commitOps) NextCommit() (int64, error) {
	return c.s.sequence.NextCommit()
}

func (c *commitOps) GetSchema(scopeID *string) *api.Schema {
	return c.s.schemaForScope(scopeID)
}

func (c *commitOps) WriteAndIndex(commit, txSeq int64, timestamp string, mergedPatch *ir.Node, txState *tx.State, lastCommit int64) (string, int64, error) {
	// Extract scope from transaction state
	var scopeID *string
	if txState != nil {
		scopeID = txState.Scope
	}

	entry := dlog.NewEntry(txState, mergedPatch, commit, timestamp, lastCommit, scopeID)
	pos, logFile, err := c.s.dLog.AppendEntry(entry)
	if err != nil {
		return "", 0, err
	}

	// Get schema for this scope
	schema := c.s.schemaForScope(scopeID)

	e := entry
	if err := index.IndexPatch(c.s.index, e, string(logFile), pos, txSeq, mergedPatch, schema, scopeID); err != nil {
		return "", 0, err
	}

	// Trigger periodic index persistence
	if c.s.indexPersister != nil {
		c.s.indexPersister.MaybePersist(commit)
	}

	// Track active scope for automatic snapshot during SwitchAndSnapshot
	if scopeID != nil {
		c.s.trackScope(*scopeID)
	}

	// Notify any registered listener about the commit
	if c.s.notifier != nil {
		kpaths := extractTopLevelKPaths(mergedPatch)
		notification := &CommitNotification{
			Commit:    commit,
			TxSeq:     txSeq,
			Timestamp: timestamp,
			KPaths:    kpaths,
			Patch:     mergedPatch,
			ScopeID:   scopeID,
		}
		c.s.notifier(notification)
	}

	return string(logFile), pos, nil
}

// extractTopLevelKPaths extracts the top-level kpaths from a patch node.
// For an object patch, returns the field names (e.g., ["users", "posts"]).
// For an array patch, returns indexed paths (e.g., ["[0]", "[1]"]).
// For keyed objects (numeric keys), returns keyed paths (e.g., ["{123}", "{456}"]).
func extractTopLevelKPaths(patch *ir.Node) []string {
	if patch == nil {
		return nil
	}

	var paths []string

	switch patch.Type {
	case ir.ObjectType:
		if len(patch.Fields) == 0 {
			return nil
		}
		// Check if this is a keyed object (numeric keys)
		if patch.Fields[0].Type == ir.NumberType {
			for _, f := range patch.Fields {
				paths = append(paths, fmt.Sprintf("{%d}", *f.Int64))
			}
		} else {
			// Regular object - string keys
			for _, f := range patch.Fields {
				paths = append(paths, f.String)
			}
		}
	case ir.ArrayType:
		for i := range patch.Values {
			paths = append(paths, fmt.Sprintf("[%d]", i))
		}
	}

	return paths
}
