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
