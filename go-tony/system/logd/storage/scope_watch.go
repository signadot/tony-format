package storage

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// A scoped watcher's per-event work, without a re-read.
//
// A baseline watcher steps a document: curDoc = Patch(curDoc, committedPatch), one patch
// per event (session.go). A scoped one could not, and head.go says why -- a scope's writes
// apply last and shadow stickily, so folding a baseline patch into a materialized scoped
// document lets baseline overwrite a leaf the scope owns. So it recomputed the whole scoped
// view per event instead.
//
// The overlay changes what has to be stepped. The scoped view is
//
//	baseline  +  overlay(T)  +  the scope's patches after T
//
// and the first term is a BASELINE document, which steps exactly the way a baseline
// watcher's does. So a scoped watcher steps baseline -- proven machinery, unchanged -- and
// derives its own view from it. Nothing here tracks ownership: the overlay is the
// ownership, and it is read once per snapshot interval rather than per event.
//
// Not safe for concurrent use; a watcher owns its stepper.
type ScopedWatchStepper struct {
	s       *Storage
	scopeID string

	baseline *ir.Node // stepped baseline document
	overlay  *ir.Node // the scope's ownership as of overlayCommit
	overlayC int64
	since    []*ir.Node // the scope's patches after overlayCommit, in commit order
}

// NewScopedWatchStepper seeds a stepper at commit. Returns nil (no error) when the scope
// cannot be served this way -- overlays off, or a scope holding keyed paths the schema does
// not declare -- so the caller keeps recomputing, which is correct and slower.
func (s *Storage) NewScopedWatchStepper(scopeID string, commit int64) (*ScopedWatchStepper, error) {
	if !s.scopeOverlay || s.scopeHasKeyedPaths(scopeID) {
		return nil, nil
	}
	base, err := s.readBaselineStateAt(commit)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = ir.Null()
	}
	w := &ScopedWatchStepper{s: s, scopeID: scopeID, baseline: base}
	if err := w.loadOverlay(commit); err != nil {
		return nil, err
	}
	return w, nil
}

// loadOverlay reads the newest overlay at or below commit and the scope's patches after it.
// This is the once-per-interval cost the per-event path is trading against.
func (w *ScopedWatchStepper) loadOverlay(commit int64) error {
	ov := w.s.latestOverlay(w.scopeID, commit)
	w.overlay, w.overlayC, w.since = nil, 0, nil
	if ov != nil {
		entry, err := w.s.dLog.ReadEntryAt(dlog.LogFileID(ov.LogFile), ov.LogPosition, ov.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("scoped watch: read overlay: %w", err)
		}
		w.overlay, w.overlayC = entry.Patch, ov.EndCommit
	}
	scope := w.scopeID
	from := w.overlayC
	for _, seg := range w.s.index.LookupRange("", &from, &commit, &scope) {
		if seg.ScopeID == nil || *seg.ScopeID != scope || isOverlaySegment(seg) {
			continue
		}
		if seg.StartCommit == seg.EndCommit || seg.EndCommit <= w.overlayC {
			continue
		}
		entry, err := w.s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("scoped watch: read scope patch: %w", err)
		}
		if entry.Patch != nil {
			w.since = append(w.since, entry.Patch)
		}
	}
	return nil
}

// Step folds one commit in and returns the scope's view after it.
//
// A BASELINE commit steps the baseline document. A commit belonging to this scope is added
// to the patches applied after the overlay -- never folded into baseline, which is what
// keeps a scope's writes from leaking into the layer they sit on top of. Another scope's
// commit changes nothing here and should not have been delivered.
func (w *ScopedWatchStepper) Step(n *CommitNotification) (*ir.Node, error) {
	switch {
	case n.ScopeID == nil:
		next, err := applyStoredPatch(w.baseline, n.Patch)
		if err != nil {
			return nil, fmt.Errorf("scoped watch: step baseline: %w", err)
		}
		w.baseline = next
	case *n.ScopeID == w.scopeID:
		w.since = append(w.since, n.Patch)
		// A new overlay may have been cut at or before this commit -- at a snapshot, which
		// happens between events -- and it subsumes what came before it. Reloading collapses
		// `since` back down; without it the list grows without bound across snapshots.
		if ov := w.s.latestOverlay(w.scopeID, n.Commit); ov != nil && ov.EndCommit > w.overlayC {
			if err := w.loadOverlay(n.Commit); err != nil {
				return nil, err
			}
		}
	}
	return w.view()
}

// view composes the scope's document from the terms.
func (w *ScopedWatchStepper) view() (*ir.Node, error) {
	doc := w.baseline
	if doc == nil {
		doc = ir.Null()
	}
	var err error
	if w.overlay != nil {
		if doc, err = applyStoredPatch(doc, w.overlay); err != nil {
			return nil, fmt.Errorf("scoped watch: apply overlay: %w", err)
		}
	}
	for _, p := range w.since {
		if doc, err = applyStoredPatch(doc, p); err != nil {
			return nil, fmt.Errorf("scoped watch: apply scope patch: %w", err)
		}
	}
	return doc, nil
}
