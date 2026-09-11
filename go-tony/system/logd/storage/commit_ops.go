package storage

import (
	"errors"
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

// StateAt is the value at kp as of commit, in the view scopeID names, as the store holds
// it: one bounded read at that path, under the write budget. It serves a precondition,
// which has been lowered to the same form (tx.LowerMatches), and the checks the write path
// makes on the array a positional write names. Nil is absent.
func (c *commitOps) StateAt(kp string, commit int64, scopeID *string) (*ir.Node, error) {
	return c.s.stateAt(commit, scopeID, kp)
}

// stateAt reads the value at kp under the write budget, and says which write budget
// refused it when one does.
func (s *Storage) stateAt(commit int64, scopeID *string, kp string) (*ir.Node, error) {
	cur, err := s.Read(commit, scopeID, kp)
	if err != nil {
		return nil, err
	}
	node, err := Collect(cur, s.writeBudget)
	if errors.Is(err, ErrBudget) {
		return nil, &WriteBudgetError{Path: kp, Budget: s.writeBudget}
	}
	return node, err
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

	commitStarted := time.Now()
	var applyTook, appendTook, indexTook time.Duration

	// Verify before storing, at each path the write names. A delta the store cannot
	// apply is not a write, it is a fault every later read replays, and nothing a
	// client sends afterwards can repair it (7cdvym1fh12ksmd5g5n0). What the log KEEPS
	// may not be what the client sent: an operation whose meaning depends on what was
	// there is applied and its RESULT diffed, at the path it was applied to, and the
	// diff is stored in its place. Both are one pass over the write's sites, each a
	// bounded read under the write budget; see lower.go.
	//
	// A patch built only from absolute operations -- which is nearly every write --
	// comes back unchanged. A nil answer means the write changed nothing, which a
	// diff can say and a patch cannot; the patch is kept so the commit still takes a
	// number and still notifies.
	stored := mergedPatch
	// Where the write is verified and lowered: a scope at what it claims, baseline at
	// what it states -- the same sites, except that baseline stops at a commented node
	// (LowerSites).
	var sites []string
	if txState != nil {
		for _, pd := range txState.PatcherData {
			if pd == nil || pd.API == nil {
				continue
			}
			if scopeID != nil {
				sites = append(sites, ClaimPaths(pd.API.Path, pd.API.Data)...)
			} else {
				sites = append(sites, LowerSites(pd.API.Path, pd.API.Data)...)
			}
		}
	}
	applyStarted := time.Now()
	lowered, err := c.s.lowerWrite(commit, mergedPatch, scopeID, sites)
	applyTook = time.Since(applyStarted)
	if err != nil {
		return "", 0, err
	}
	if lowered != nil {
		stored = lowered
	}

	// The notification is the STORED delta, copied, and delivered in the client's
	// vocabulary: a keyed array is an array to a watcher and an object of names in the
	// log (raise.go). A replay reads the same entry back and raises it the same way, so
	// live and replay are the same bytes (one_delta_shape.md).
	notification := newCommitNotification(commit, txSeq, timestamp, stored, scopeID)
	notification.Patch = c.s.raiseDelta(scopeID, notification.Patch)

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

	// Get current generation for indexing
	generation := c.s.dLog.GetGeneration(logFile)

	e := entry
	indexStarted := time.Now()
	// The STORED delta is what a rebuild reads back, so it is what the live index
	// has to agree with: index.Build needs no schema only because the two are the
	// same node.
	index.IndexPatch(c.s.index, e, string(logFile), pos, txSeq, generation, stored, scopeID)
	indexTook = time.Since(indexStarted)

	// Dual-write: also index to pending index if migration is in progress. This one
	// CompleteMigration installs as the live index verbatim, so it has to see every
	// commit the active index saw; IndexPatch cannot fail, which is what makes the two
	// writes here either both done or neither reached.
	if pendingIdx := c.s.schema.GetPendingIndex(); pendingIdx != nil {
		index.IndexPatch(pendingIdx, e, string(logFile), pos, txSeq, generation, stored, scopeID)
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
