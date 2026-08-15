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

// MatchStateAt serves a precondition read from the stepped head when it can.
//
// Baseline is served from the stepped head. A scope cannot be stepped the same way -- its
// writes apply last and shadow baseline stickily, so folding a baseline patch into a
// materialized scoped document lets baseline overwrite a leaf the scope owns (issue
// 9b2vpggxh) -- but with an overlay stating that ownership explicitly, the scoped view can
// be rebuilt on top of the baseline head instead of on top of a fresh baseline read.
//
// doCommit holds commitMu across match evaluation, which is what makes reading the head
// here safe — it is the same lock stepHead is written under.
func (c *commitOps) MatchStateAt(kpath string, commit int64, scopeID *string) (*ir.Node, error) {
	if scopeID != nil {
		// Not a full read any more: with an overlay stating the scope's ownership, the
		// scoped view can be built ON TOP of the baseline head rather than replaying
		// baseline from the last snapshot. See scopedHeadStateAt, which falls back to a
		// full read whenever it cannot be sure.
		return c.s.scopedHeadStateAt(commit, scopeID)
	}
	return c.s.headStateAt(commit)
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

	// Under DurabilitySync, flush before indexing: the index is what makes a commit
	// readable, so syncing first means the index never points at a record that is not
	// yet on stable storage. Under the default DurabilityOS this is skipped and the
	// record is durable only once the OS flushes it (see Durability).
	if c.s.durability == DurabilitySync {
		if err := c.s.dLog.Sync(logFile); err != nil {
			return "", 0, fmt.Errorf("failed to sync log after append: %w", err)
		}
	}

	// Get schema for this scope
	schema := c.s.schemaForScope(scopeID)

	// Get current generation for indexing
	generation := c.s.dLog.GetGeneration(logFile)

	e := entry
	if err := index.IndexPatch(c.s.index, e, string(logFile), pos, txSeq, generation, mergedPatch, schema, scopeID); err != nil {
		return "", 0, err
	}

	// Dual-write: also index to pending index if migration is in progress
	if pendingIdx := c.s.schema.GetPendingIndex(); pendingIdx != nil {
		pendingSchemaParsed := c.s.schema.GetPendingParsed()
		if err := index.IndexPatch(pendingIdx, e, string(logFile), pos, txSeq, generation, mergedPatch, pendingSchemaParsed, scopeID); err != nil {
			return "", 0, fmt.Errorf("failed to index to pending: %w", err)
		}
	}

	// A scoped write can add a keyed path the schema does not declare, which is what
	// decides whether this scope can be served from an overlay at all. Decided from the
	// patch rather than by re-reading the index -- see noteScopeKeyedWrite.
	if scopeID != nil {
		c.s.noteScopeKeyedWrite(*scopeID, mergedPatch)
	}

	// Trigger periodic index persistence
	if c.s.indexPersister != nil {
		c.s.indexPersister.MaybePersist(commit)
	}

	// The entry is now in the log and in the index, so it is readable: publish it. This
	// runs under the commit lock (doCommit holds it across this call), which is what
	// makes the watermark and the notification queue both ordered by commit. The fan-out
	// itself happens later, on the tick's dispatcher goroutine, so a slow notifier still
	// cannot serialize commits.
	notification := newCommitNotification(commit, txSeq, timestamp, mergedPatch, scopeID)

	// Step the head by this commit before publishing, while still under the commit lock,
	// so the head is current for the next precondition. The notification's patch is the
	// stripped copy — the merged patch still carries !logd-patch-root tags, which must
	// not reach a document a precondition is matched against.
	//
	// A scoped write steps with no patch: LookupRange filters by scope, so a scoped entry
	// is not part of baseline state, but it does take a commit number, and the head has
	// to follow that number or the next step reads as a gap.
	headPatch := notification.Patch
	if scopeID != nil {
		headPatch = nil
	}
	c.s.stepHead(commit, headPatch)

	c.s.tick.publish(commit, notification)

	return string(logFile), pos, nil
}

// LockCommit acquires the storage-wide commit lock; the returned func releases it.
func (c *commitOps) LockCommit() func() {
	c.s.commitMu.Lock()
	return c.s.commitMu.Unlock
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

	// A comment wraps the value it precedes, and this asks what KIND of node the
	// patch is: a comment is not a kind of container (3cdjz00jh12krns4g1n0).
	patch = ir.Uncomment(patch)

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
