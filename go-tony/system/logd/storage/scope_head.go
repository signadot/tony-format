package storage

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
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
// The overlay is what makes it usable. It states the scope's ownership explicitly, so the
// scoped view at the head commit is
//
//	baseline head  +  overlay(T)  +  the scope's patches after T
//
// and the first term is already in memory. What is left to replay is bounded by the
// snapshot interval on the scope's own writes alone -- the baseline replay, which was the
// larger half, goes away entirely.
//
// Callers MUST hold commitMu: headStateAt does, and the head must not move underneath the
// terms applied to it.

// scopedHeadStateAt returns the scoped document at commit for evaluating a precondition.
//
// It falls back to a full read whenever it cannot be sure of the shortcut: overlays off,
// no overlay yet for this scope, a scope the overlay cannot serve, or a commit the head
// cannot answer for. Falling back is correct and merely slower, which is the right way
// round for a path that decides whether a write lands.
func (s *Storage) scopedHeadStateAt(commit int64, scopeID *string) (*ir.Node, error) {
	// The scope's own kept document, when it is current, is the whole answer: no
	// overlay to read, no patches to replay, nothing of baseline to fold. See
	// scopeHeadDoc.
	if scopeID != nil {
		if doc, ok := s.scopeHeadAt(commit, *scopeID); ok {
			return doc, nil
		}
	}
	if !s.scopeOverlay || scopeID == nil || s.scopeHasKeyedPaths(*scopeID) {
		return s.ReadStateAt("", commit, scopeID)
	}
	ov := s.latestOverlay(*scopeID, commit)
	if ov == nil {
		return s.ReadStateAt("", commit, scopeID)
	}
	if !s.headSeeded || s.headCommit != commit {
		// headStateAt would seed it with a full baseline read, which is exactly the cost
		// this exists to avoid paying twice. Let the ordinary read answer, and the head
		// will be current by the next precondition.
		return s.ReadStateAt("", commit, scopeID)
	}

	doc := s.head
	if doc == nil {
		doc = ir.Null()
	}

	overlayEntry, err := s.dLog.ReadEntryAt(dlog.LogFileID(ov.LogFile), ov.LogPosition, ov.LogFileGeneration)
	if err != nil {
		return nil, fmt.Errorf("scoped precondition: read overlay: %w", err)
	}
	if overlayEntry.Patch != nil {
		if doc, err = applyStoredPatch(doc, overlayEntry.Patch); err != nil {
			return nil, fmt.Errorf("scoped precondition: apply overlay: %w", err)
		}
	}

	after := ov.EndCommit
	for _, seg := range s.index.LookupRange("", &after, &commit, scopeID) {
		if seg.ScopeID == nil || *seg.ScopeID != *scopeID || isOverlaySegment(seg) {
			continue
		}
		if seg.StartCommit == seg.EndCommit || seg.EndCommit <= ov.EndCommit {
			continue
		}
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return nil, fmt.Errorf("scoped precondition: read scope patch: %w", err)
		}
		if entry.Patch == nil {
			continue
		}
		if doc, err = applyStoredPatch(doc, entry.Patch); err != nil {
			return nil, fmt.Errorf("scoped precondition: apply scope patch: %w", err)
		}
	}
	return doc, nil
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
// The overlay above made a scoped precondition cost the scope's writes since the
// last overlay instead of a replay of everything. That is a bound on REPLAY, and it
// is not a bound on how often the replay happens: every scoped write pays it, and
// between overlays it grows. Once a write is verified before it is stored
// (verifyApplies), every scoped write pays it, not only the conditional ones --
// which is what made a burst of scoped writes quadratic (sb33w8p9h12kr16kg5n0).
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
