package storage

import (
	"fmt"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

// The head document: the baseline state at the last commit, kept and stepped forward
// rather than rebuilt.
//
// A CAS precondition has to be evaluated against current state, and doing that with
// ReadStateAt costs a snapshot read plus every patch since — O(patches since the last
// snapshot), paid inside commitMu, per conditional write. That is what made a conditional
// patch 60-500x an unconditional one (issue x2bn8w56h), and why its cost was erratic
// rather than proportional to the document: the replay length is a sawtooth that resets
// at each snapshot.
//
// This is the same remedy the baseline watch path already took (session.go: "It replaces
// a full ReadStateAt per event per watcher, which was O(patches since the last snapshot):
// 1.6ms at 50 commits, 62ms at 1550"). Nothing is kept that was not already built — the
// old code materialized a whole document per conditional write and threw it away.
//
// Two properties of tony.Patch make this cheap and safe, both checked rather than assumed:
// it does not mutate the document it is given, so an earlier head stays valid for anyone
// still holding it; and it shares untouched subtrees with the result, so a step costs the
// size of the patch and not the size of the document.
//
// Scoped views are deliberately not kept. A scope's writes apply LAST and shadow baseline
// stickily, so folding a baseline patch into a materialized scoped document would let
// baseline overwrite a leaf the scope owns — the same reason a scoped watcher cannot step.
// Scoped preconditions keep recomputing; issue 9b2vpggxh.

// headStateAt returns the baseline document at commit for evaluating a precondition,
// seeding the head if it is not yet current.
//
// Callers MUST hold commitMu and MUST NOT mutate the result: untouched subtrees are
// shared with earlier heads and with any watcher document stepped from the same patches,
// so a mutation here would surface in all of them.
//
// A commit other than headCommit means the head cannot answer for it, so this falls back
// to a full read rather than returning state from the wrong commit.
func (s *Storage) headStateAt(commit int64) (*ir.Node, error) {
	if s.headSeeded && s.headCommit == commit {
		return s.head, nil
	}
	doc, err := s.readBaselineStateAt(commit)
	if err != nil {
		return nil, err
	}
	s.head, s.headCommit, s.headSeeded = doc, commit, true
	return doc, nil
}

// stepHead advances the head by one commit's patch.
//
// patch must be the notification's stripped copy, not the merged patch the commit path
// holds: the merged patch still carries the !logd-patch-root tags TagPatchRoots put on it,
// and those would end up on nodes in the head, where a precondition's tony.Match would
// see them. newCommitNotification already builds the stripped form, and it is what a
// baseline watcher steps with.
//
// On any failure the head is dropped rather than left behind: a head that has silently
// diverged is worse than no head, and dropping it costs one full read on the next
// precondition, which is what every conditional write paid before this existed.
//
// Callers MUST hold commitMu.
func (s *Storage) stepHead(commit int64, patch *ir.Node) {
	if !s.headSeeded {
		// Nothing to step. The next precondition seeds it at whatever commit it asks
		// for, which by then includes this one.
		return
	}
	if s.headCommit != commit-1 {
		// A gap means some commit did not step the head, so stepping now would skip
		// it. Drop and re-seed.
		s.dropHead("commit gap", fmt.Errorf("head at %d, stepping %d", s.headCommit, commit))
		return
	}
	if patch == nil {
		s.headCommit = commit // nothing to apply; the state is unchanged
		return
	}
	// An empty document reads back as nil, and null is what the read path's own
	// empty-base branch folds onto, so it is the base to step from.
	base := s.head
	if base == nil {
		base = ir.Null()
	}
	stepped, err := tony.Patch(base, patch)
	if err != nil {
		s.dropHead("patch failed", err)
		return
	}
	s.head, s.headCommit = stepped, commit
}

// dropHead forgets the head so the next read re-seeds from the log. Callers MUST hold
// commitMu.
func (s *Storage) dropHead(why string, err error) {
	if s.logger != nil {
		s.logger.Error("dropping stepped head document; re-seeding on next use",
			"reason", why, "error", err, "headCommit", s.headCommit)
	}
	s.head, s.headCommit, s.headSeeded = nil, 0, false
}

// CheckHead compares the stepped head against a full read at the same commit and drops it
// if they disagree, so drift is bounded by how often this is called rather than by how
// long the process runs.
//
// The invariant crosses two appliers: the read path applies the stored, still-tagged
// entries through the streaming processor, while the head steps the stripped copy through
// tony.Patch. They are meant to agree — the baseline watchers already depend on it, and
// read_equivalence_test names the tony.Patch fold "the semantics of record" — so this is
// a guard against drift, not a routine correction.
//
// Called at snapshot time. Unlike the rest of this file it takes commitMu itself, and
// only to copy two words: the comparison read runs with the lock released, so it does not
// stall commits for the size of the document. That is safe because tony.Patch does not
// mutate what it is given, so the captured head keeps its value however far the live head
// advances past it in the meantime.
func (s *Storage) CheckHead() {
	s.commitMu.Lock()
	head, commit, seeded := s.head, s.headCommit, s.headSeeded
	s.commitMu.Unlock()
	if !seeded {
		return
	}

	want, err := s.readBaselineStateAt(commit)
	if err == nil && nodeEqual(head, want) {
		return
	}

	why, reason := "stepped head diverged from a full read", error(nil)
	if err != nil {
		why, reason = "check read failed", err
	}
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	// The live head may have advanced past the one compared. Dropping it anyway costs
	// one re-seed and keeps this to a single rule: if a check ever failed, do not keep
	// serving from a head whose history includes it.
	s.dropHead(why, reason)
}

// nodeEqual compares two possibly-nil documents. Empty state reads back as nil, so the
// nil cases are ordinary rather than exceptional.
func nodeEqual(a, b *ir.Node) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.DeepEqual(b)
}
