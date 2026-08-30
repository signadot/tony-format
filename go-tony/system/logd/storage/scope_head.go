package storage

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// A scoped CAS precondition, served without re-reading baseline.
//
// head.go keeps the baseline document at headCommit and steps it, so a baseline
// precondition costs the size of a patch rather than a replay from the last snapshot. A
// scoped precondition could not use it: a scope's view is baseline with the scope's own
// writes applied last, and folding a baseline patch into a materialized scoped document
// lets baseline overwrite a leaf the scope owns.
//
// So a scope keeps its OWN document. When it is current at the commit being asked about it
// is the whole answer -- nothing to replay, nothing of baseline to fold; when it is not,
// the ordinary read answers. See scopeHeadDoc.
//
// Callers MUST hold commitMu: steppedBaselineAt does, and the head must not move underneath the
// terms applied to it.

// steppedScopedAt: a scope, whole document, STEPPED -- the scope's own kept document. See
// read.go for the axes. This is what a scoped CAS precondition is evaluated against.
//
// It falls back to a full read for any commit the kept document cannot answer for.
// Falling back is correct and merely slower, which is the right way round for a path that
// decides whether a write lands.
func (s *Storage) steppedScopedAt(commit int64, scopeID *string) (*ir.Node, error) {
	if scopeID != nil {
		if doc, ok := s.scopeHeadAt(commit, *scopeID); ok {
			return doc, nil
		}
	}
	return s.ReadStateAt("", commit, scopeID)
}

// applyStoredPatch folds one stored patch into a document.
//
// The stored form still carries the !logd-patch-root tags TagPatchRoots put on it, which
// are the streaming processor's markers and not part of any value -- so they are stripped
// from a copy first, exactly as session.go does before stepping a baseline watch document.
// tony.Patch is then the same fold the read path performs; read_equivalence_test calls it
// the semantics of record.
func applyStoredPatch(doc, patch *ir.Node) (*ir.Node, error) {
	p := patch.DeepCopy()
	tx.StripPatchRootTagRecursive(p)
	if doc == nil {
		doc = ir.Null()
	}
	next, err := api.NextState(doc, p)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return ir.Null(), nil
	}
	return next, nil
}

// A scope's own stepped document.
//
// Bounding the REPLAY is not enough, and that is the whole reason this exists. A bound on
// how much gets replayed is not a bound on how often: since a write is verified before it
// is stored (verifyApplies), EVERY scoped write pays a read, not only the conditional
// ones, which is what made a burst of scoped writes quadratic (sb33w8p9h12kr16kg5n0). The
// answer has to be a document that is already there, not a cheaper way to rebuild one.
//
// So a scope keeps a document too. The reason baseline's trick does not transfer is
// specific and does not apply here: folding a BASELINE patch into a materialized
// scoped document lets baseline overwrite a leaf the scope owns, because a scope's
// writes apply last (9b2vpggxh12ks0qde5n0). Folding the scope's OWN patch does not
// have that problem -- it applies last, which is exactly where it belongs.
//
// The scoped view at C-1 is fold(baseline<=C-1) then fold(scope<=C-1). A scoped
// commit at C adds one more scope patch at the end of the second fold, which is
// what stepping it does. The condition is that the kept document is at exactly C-1,
// and that is self-checking, because commits are numbered globally: a baseline
// commit, or another scope's, takes the number in between and leaves this scope's
// document at C-2, where it is not used.
//
// Callers MUST hold commitMu, and MUST NOT mutate what they get back: subtrees are
// shared with earlier documents, exactly as the baseline head's are.

type scopeHeadDoc struct {
	doc    *ir.Node
	commit int64
}

// scopeHeadAt returns the kept scoped document if it is current at commit.
func (s *Storage) scopeHeadAt(commit int64, scopeID string) (*ir.Node, bool) {
	kept := s.scopeHeads[scopeID]
	if kept == nil || kept.commit != commit {
		return nil, false
	}
	return kept.doc, true
}

// installScopeHead keeps the document a scoped commit was verified against, and
// forgets the ones which can no longer answer for anything.
//
// A kept document is usable only at exactly the commit before the next one, so any
// entry older than that is dead: a scope which stopped writing cannot come back to
// its own document, it has to be rebuilt. Dropping them here bounds what is held to
// the scopes writing right now, rather than to every scope the process has seen.
func (s *Storage) installScopeHead(commit int64, scopeID string, doc *ir.Node) {
	if s.scopeHeads == nil {
		s.scopeHeads = map[string]*scopeHeadDoc{}
	}
	for id, kept := range s.scopeHeads {
		if id != scopeID && kept.commit < commit-1 {
			delete(s.scopeHeads, id)
		}
	}
	s.scopeHeads[scopeID] = &scopeHeadDoc{doc: doc, commit: commit}
}

// forgetScopeHead drops a scope's kept document. Called where a scope stops being a
// thing that has one -- DeleteScope -- so nothing holds a document for a scope which
// no longer exists.
func (s *Storage) forgetScopeHead(scopeID string) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	delete(s.scopeHeads, scopeID)
}
