package storage

import (
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// The property one_delta_shape.md is named for, asserted at the commit rather than in a
// library.
//
// IDENTITY. For any commit, the delta a live watcher is handed and the delta a replaying
// watcher is handed are the same bytes, and both are the stored entry raised into the
// client's vocabulary. Not equivalent: the same. There is one delta per commit, the log
// keeps it, and every delivery is a copy of it.
//
// ABSOLUTENESS. Applied to the state at C-1, the delta gives the state at C, comments
// counted. A relative operation is lowered before it is stored, so what is delivered is
// what happened, and a watcher that folds it from C-1 lands where a read at C lands.
//
// The writes are chosen so the stored delta is not always what the client sent: an
// !insert at an index is relative and lowered, a comment travels as a wrapper, a write
// that changes nothing still commits, a scope's write is delivered in the scope's view.
// A notification built from the merged patch instead of the stored entry parts from the
// replay at the lowered write, which is the row this exists to hold.
func TestDeltaIdentity(t *testing.T) {
	scope := "s1"
	writes := []struct {
		path  string
		src   string
		scope *string
	}{
		{"", `{votes: [{by: scott}], d: {k0: 1}}`, nil},
		{"votes[1]", `!insert {by: ana}`, nil},
		{"d.e", "# note\n{k2: {nested: 2}}", nil},
		{"d.k0", `1`, nil},
		{"d.e.k2", `{nested: 3}`, &scope},
		{"d", `!delete`, nil},
		{"votes[0]", `!delete {by: scott}`, nil},
	}
	for _, lowered := range []bool{false, true} {
		name := "as shipped"
		if lowered {
			name = "lowering everything"
		}
		t.Run(name, func(t *testing.T) {
			s := openTestStorage(t)
			s.lowerEverything(lowered)

			var mu sync.Mutex
			live := map[int64]*CommitNotification{}
			s.SetCommitNotifier(func(n *CommitNotification) {
				mu.Lock()
				defer mu.Unlock()
				live[n.Commit] = n
			})

			var commits []int64
			for _, w := range writes {
				var c int64
				if w.scope != nil {
					c = commitAt(t, s, w.scope, w.path, w.src)
				} else {
					var err error
					if c, err = applyOp(t, s, genOp{path: w.path, src: w.src}); err != nil {
						t.Fatalf("write %q at %q: %v", w.src, w.path, err)
					}
				}
				commits = append(commits, c)
			}
			s.tick.waitDrained()
			mu.Lock()
			defer mu.Unlock()

			for i, c := range commits {
				scopeID := writes[i].scope
				got := live[c]
				if got == nil {
					t.Fatalf("commit %d: no live notification", c)
				}
				liveBytes := withComments(got.Patch)

				// IDENTITY, live against replay: the one commit, replayed in its own view.
				var replayed *CommitNotification
				err := eachPatchInRange(s, "", c, c, scopeID, func(n *CommitNotification) error {
					if n.Commit == c {
						replayed = n
					}
					return nil
				})
				if err != nil {
					t.Fatalf("commit %d: replay: %v", c, err)
				}
				if replayed == nil {
					t.Fatalf("commit %d: not replayed", c)
				}
				if replayBytes := withComments(replayed.Patch); replayBytes != liveBytes {
					t.Errorf("commit %d (%s at %q): live and replay differ\n live   %s\n replay %s",
						c, writes[i].src, writes[i].path, liveBytes, replayBytes)
				}

				// IDENTITY, against the log: both are the stored entry, raised.
				stored := storedDelta(t, s, c, scopeID)
				if want := withComments(s.raiseDelta(scopeID, deliverable(stored), c)); want != liveBytes {
					t.Errorf("commit %d (%s at %q): live is not the stored entry raised\n live   %s\n stored %s",
						c, writes[i].src, writes[i].path, liveBytes, want)
				}

				// ABSOLUTENESS: folded from C-1, the delta lands where the read at C lands.
				var before *ir.Node
				if i > 0 {
					before, err = readStateAt(s, "", c-1, scopeID)
					if err != nil {
						t.Fatalf("commit %d: read at %d: %v", c, c-1, err)
					}
				}
				after, err := readStateAt(s, "", c, scopeID)
				if err != nil {
					t.Fatalf("commit %d: read: %v", c, err)
				}
				folded, err := applyStoredPatch(before, got.Patch)
				if err != nil {
					t.Fatalf("commit %d: fold: %v", c, err)
				}
				if !api.SameState(folded, after) {
					t.Errorf("commit %d (%s at %q): the fold from %d does not land at %d\n folded %s\n read   %s",
						c, writes[i].src, writes[i].path, c-1, c, withComments(folded), withComments(after))
				}
			}
		})
	}
}

// storedDelta reads the entry the log keeps for commit, as the index finds it.
func storedDelta(t *testing.T, s *Storage, commit int64, scopeID *string) *ir.Node {
	t.Helper()
	for _, seg := range segmentsAt(s.index, "", &commit, &commit, scopeID) {
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			t.Fatalf("commit %d: read entry: %v", commit, err)
		}
		if entry.Commit == commit {
			return entry.Patch
		}
	}
	t.Fatalf("commit %d: no segment in the index", commit)
	return nil
}
