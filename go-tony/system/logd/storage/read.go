package storage

import "github.com/signadot/tony-format/go-tony/ir"

// Reading, and which read answers which question.
//
// There are nine ways to read state here and no one of them is wrong; what was wrong was
// that their names did not say which question they answer, so a caller reached one and paid
// for another. Three of one month's performance bugs were that (ap8ddvp2h12krd43gdn0,
// kds4sx3bh12krdrkghn0, ntadpaech12krandgsn0). Every read varies on three axes:
//
//	VIEW    baseline, or a scope. A scope is a live overlay -- baseline as of the commit,
//	        with the scope's own writes applied last -- not a branch.
//	EXTENT  the whole document, or the subtree at a path. A subtree read is answered from
//	        the snapshot's path index and the deltas which touch it; a whole read replays
//	        everything since the last snapshot.
//	SOURCE  REPLAYED from the last snapshot, or STEPPED from a document the store keeps
//	        current. Stepping costs the size of one patch; replaying costs every patch
//	        since the snapshot. The commit path steps, because it reads on every write.
//
// The set, by those axes:
//
//	                    whole document                 subtree at a path
//	  baseline,         replayBaselineAt               narrowSubtreeAt
//	  replayed
//	  scope, replayed   replayScopedAt                 (none: a scope reads whole)
//	  baseline,         steppedBaselineAt              (none: the head is whole)
//	  stepped
//	  scope, stepped    steppedScopedAt                (none)
//
// And the entry points a caller reaches for, which choose among them:
//
//	ReadStateAt          the state at a commit, whole, in whichever view the scope says.
//	                     What a client read used to be answered with, and still is when
//	                     nothing narrower applies.
//	ReadSubtreeRootedAt  the subtree at a path, rooted as the whole read would have been,
//	                     when the store can read it that way -- and declining rather than
//	                     guessing when it cannot.
//	ReadSubtreeAt        the same, as the value at the path rather than rooted.
//	AbsentSpineAt        no read at all: the index proves the path was never written, and
//	                     the answer is a document which resolves exactly that far.
//	steppedStateAt       what a WRITE is applied to, from the kept document. The commit
//	                     path's read, and the only one that must never replay.
//
// A caller which does not know which to use wants ReadStateAt, and a caller on the commit
// path wants steppedStateAt. Everything else is one of those two, done more cheaply when
// the store can prove it is allowed to.

// ReadStateAt reads the state at a commit, whole, in the view the scope names: baseline
// when scopeID is nil, and otherwise baseline with that scope's writes on top.
//
// kp does NOT narrow the result. Log entries are whole-document patches, so what comes back
// is the document, root-rooted -- a SUPERSET of kp's subtree, which callers trim (see the
// session's readDocAt). kp is kept in the signature because it is the read's declared
// subject and what the counters record it as; it is deliberately not used to pick the
// snapshot base or the patch range, because doing so silently returned no snapshot for
// every non-root read (bvm163tyh12krwcqcsn0) and applied each entry once per level of kp.
//
// For a read which does narrow, see ReadSubtreeRootedAt.
func (s *Storage) ReadStateAt(kp string, commit int64, scopeID *string) (*ir.Node, error) {
	if scopeID != nil {
		return s.replayScopedAt(commit, scopeID)
	}
	return s.replayBaselineAt(commit)
}

// steppedStateAt is the state a write at the next commit is applied to: the kept baseline
// head, or for a scope the same view its own reads see.
//
// It is the commit path's read, so it must never replay: a store which replayed here paid
// O(patches since the snapshot) per write, which is what the head exists to remove (see
// head.go, and x2bn8w56h for what it cost).
func (s *Storage) steppedStateAt(commit int64, scopeID *string) (*ir.Node, error) {
	if scopeID != nil {
		return s.steppedScopedAt(commit, scopeID)
	}
	return s.steppedBaselineAt(commit)
}
