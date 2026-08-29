package storage

import (
	"fmt"
	"time"

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
		// baseline from the last snapshot. See steppedScopedAt, which falls back to a
		// full read whenever it cannot be sure.
		return c.s.steppedScopedAt(commit, scopeID)
	}
	return c.s.steppedBaselineAt(commit)
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

// LowersScopeWrites reports whether lowerWrite will turn a scoped write into a claim.
func (c *commitOps) LowersScopeWrites() bool { return c.s.lowering }

func (c *commitOps) WriteAndIndex(commit, txSeq int64, timestamp string, mergedPatch *ir.Node, txState *tx.State, lastCommit int64) (string, int64, error) {
	// Extract scope from transaction state
	var scopeID *string
	if txState != nil {
		scopeID = txState.Scope
	}

	// The notification's patch is the stripped copy — the merged patch still carries
	// !logd-patch-root tags, which must not reach a document a precondition is
	// matched against. It is built here, before the write, because the check below
	// needs it.
	notification := newCommitNotification(commit, txSeq, timestamp, mergedPatch, scopeID)
	commitStarted := time.Now()
	var applyTook, appendTook, indexTook time.Duration

	// Verify before storing. A delta the store cannot apply is not a write, it is a
	// fault every later read replays, and nothing a client sends afterwards can
	// repair it. This apply used to happen after the entry was written, in stepHead,
	// where its only possible answer was to drop the head (7cdvym1fh12ksmd5g5n0).
	//
	// The result is kept: for baseline it IS the next head, so verifying costs the
	// step that was going to happen anyway.
	applyStarted := time.Now()
	base, stepped, err := c.s.verifyApplies(commit, notification.Patch, scopeID)
	applyTook = time.Since(applyStarted)
	if err != nil {
		return "", 0, err
	}

	// What the log KEEPS may not be what the client sent. A patch carrying an
	// operation whose meaning depends on what was there is applied and its RESULT
	// diffed, and the diff is stored in its place -- so what a later read re-applies
	// states what the value is rather than how it once related to something. Both
	// sides of that diff are the two the verification above just produced, so this
	// costs a diff and no read. See lower.go.
	//
	// A patch built only from absolute operations -- which is nearly every write --
	// comes back unchanged. A nil answer means the write changed nothing, which a
	// diff can say and a patch cannot; the patch is kept so the commit still takes a
	// number and still notifies.
	stored := mergedPatch
	// The paths the client named, which is what a scope claims. Baseline does not
	// use them: it stores the difference, having nothing to own.
	var writePaths []string
	if txState != nil {
		for _, pd := range txState.PatcherData {
			if pd != nil && pd.API != nil {
				writePaths = append(writePaths, ClaimPath(pd.API.Path, pd.API.Data))
			}
		}
	}
	if lowered, err := c.s.lowerWrite(base, stepped, mergedPatch, scopeID != nil, writePaths); err != nil {
		return "", 0, err
	} else if lowered != nil {
		stored = lowered
	}

	entry := dlog.NewEntry(txState, stored, commit, timestamp, lastCommit, scopeID)
	appendStarted := time.Now()
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
	appendTook = time.Since(appendStarted)

	// Get schema for this scope
	schema := c.s.schemaForScope(scopeID)

	// Get current generation for indexing
	generation := c.s.dLog.GetGeneration(logFile)

	e := entry
	indexStarted := time.Now()
	// The STORED delta is what a rebuild reads back, so it is what the live index
	// has to agree with: index.Build's "we rely on !key tags stored in the patches"
	// is only true when the two are the same node.
	if err := index.IndexPatch(c.s.index, e, string(logFile), pos, txSeq, generation, stored, schema, scopeID); err != nil {
		return "", 0, err
	}
	indexTook = time.Since(indexStarted)

	// Dual-write: also index to pending index if migration is in progress
	if pendingIdx := c.s.schema.GetPendingIndex(); pendingIdx != nil {
		pendingSchemaParsed := c.s.schema.GetPendingParsed()
		if err := index.IndexPatch(pendingIdx, e, string(logFile), pos, txSeq, generation, stored, pendingSchemaParsed, scopeID); err != nil {
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
	//
	// Install the head this commit was verified against, before publishing, so it is
	// current for the next precondition.
	c.s.installHead(commit, stepped, scopeID)

	c.s.tick.publish(commit, notification)

	// What this commit spent its time on, for the report and for a line in the log
	// when it was slow. A write is milliseconds when nothing is wrong, and from
	// outside a slow write and a queued one look the same (dvgz9308h12ks4xmgdn0).
	c.s.noteCommit(patchPathHint(mergedPatch), applyTook, appendTook, indexTook, time.Since(commitStarted))

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
