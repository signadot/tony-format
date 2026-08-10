package storage

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// SPIKE — bounded scope overlay, non-keyed data only. See docs/scope_overlay_plan.md.
//
// A scope's writes replay in full on every read, forever, because they are exempt from
// both snapshotting and compaction. The overlay is the scope's ownership, materialized at
// a commit T and written beside the baseline snapshot, so a scoped read replays only what
// the scope has written since T.
//
//	overlay(T) = unconditional(Diff(baseline@T, scoped@T))  ∪  an entry per owned path
//
// What this spike leaves out, deliberately: keyed arrays (they need the annotation
// pre-pass, plan R2/P1), compaction of the patches the overlay subsumes (plan phase 2),
// and a durable marker — see scopeOverlayTx.

// scopeOverlayTx marks an index segment as an overlay rather than a scope patch.
//
// SPIKE-GRADE. It survives in the live index but not a rebuild: index.Build takes the tx
// from entry.TxSource, which an overlay has none of, so a rebuilt index reads 0. A real
// implementation wants a field on dlog.Entry, which means codegen. Everything else here
// is the shape the plan describes.
const scopeOverlayTx = int64(-1)

// EnableScopeOverlay turns the overlay read path on. Off by default: with it off, nothing
// in this file runs and scoped reads replay as before.
func (s *Storage) EnableScopeOverlay(v bool) { s.scopeOverlay = v }

// isOverlaySegment reports whether seg is an overlay rather than one of the scope's own
// patches.
func isOverlaySegment(seg index.LogSegment) bool {
	return seg.ScopeID != nil && seg.EndTx == scopeOverlayTx
}

// BuildScopeOverlay computes the overlay for a scope at commit, without writing it.
//
// The two states come from the REPLAY path, which is the definition being compressed. The
// values for owned paths are read out of the scoped state rather than captured when each
// write happened: at build time the scoped state already reflects every scope write, so
// the two agree. (Stepping is where captured-at-write matters, because there the live
// document has had a baseline delta folded in first.)
func (s *Storage) BuildScopeOverlay(scopeID string, commit int64) (*ir.Node, error) {
	base, err := s.readBaselineStateAt(commit)
	if err != nil {
		return nil, fmt.Errorf("overlay: baseline read: %w", err)
	}
	scoped, err := s.readScopedStateAtReplay(commit, &scopeID)
	if err != nil {
		return nil, fmt.Errorf("overlay: scoped read: %w", err)
	}
	if base == nil {
		base = ir.Null()
	}
	if scoped == nil {
		scoped = ir.Null()
	}

	overlay := unconditionalPatch(tony.Diff(base.Clone(), scoped.Clone()))

	// A minimal diff records only where the two states differ, so a scope that wrote the
	// value baseline already holds records nothing and loses the path. The index knows
	// what it wrote; re-state each of those from the scoped view. Plan R3.
	for _, p := range s.scopeOwnedLeafPaths(scopeID) {
		v, err := scoped.GetPath("$." + p)
		if err != nil || v == nil {
			continue // absent in the scoped view: the diff's tombstone is what holds it
		}
		rooted, err := tx.RootPatchAt(p, v.Clone())
		if err != nil {
			return nil, fmt.Errorf("overlay: rooting %q: %w", p, err)
		}
		if overlay == nil {
			overlay = rooted
			continue
		}
		if overlay, err = tony.Patch(overlay, rooted); err != nil {
			return nil, fmt.Errorf("overlay: merging owned path %q: %w", p, err)
		}
	}
	return overlay, nil
}

// scopeOwnedLeafPaths returns the paths the scope has written, keeping only the deepest of
// any chain: an ancestor path in the index names a subtree the scope only partly wrote, so
// asserting it would take baseline's siblings into the scope's ownership too.
func (s *Storage) scopeOwnedLeafPaths(scopeID string) []string {
	seen := map[string]bool{}
	for _, seg := range s.index.AllSegments() {
		if seg.ScopeID == nil || *seg.ScopeID != scopeID || isOverlaySegment(seg) {
			continue
		}
		if seg.KindedPath != "" {
			seen[seg.KindedPath] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		if !hasDescendant(p, seen) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths
}

func hasDescendant(p string, all map[string]bool) bool {
	for q := range all {
		if q != p && strings.HasPrefix(q, p+".") {
			return true
		}
	}
	return false
}

// WriteScopeOverlay computes the overlay at commit and appends it as a scope-tagged log
// entry, indexed like any patch so its paths stay addressable.
//
// It spans [commit-1, commit], the same shape a patch at that commit has, NOT [0, commit].
// Spanning from zero looks right -- it sorts the overlay before every scope patch -- but
// the index tree is ORDERED by StartCommit while rangeFunc prunes on EndCommit, so an
// overlay parked at position 0 while claiming to be at T is invisible to any bounded
// query: LookupRange with from=T-1 does not return it, though its EndCommit is exactly T.
// That forces every lookup to scan from zero, which was the whole cost this is meant to
// remove. Sharing a patch's shape keeps position and predicate in agreement, and it still
// orders before everything the overlay does not subsume, which is all that is required.
func (s *Storage) WriteScopeOverlay(scopeID string, commit int64) error {
	if commit <= 0 {
		return nil
	}
	overlay, err := s.BuildScopeOverlay(scopeID, commit)
	if err != nil {
		return err
	}
	if overlay == nil {
		return nil // the scope holds nothing of its own
	}

	entry := dlog.NewEntry(nil, overlay, commit, time.Now().UTC().Format(time.RFC3339), commit-1, &scopeID)
	pos, logFile, err := s.dLog.AppendEntry(entry)
	if err != nil {
		return fmt.Errorf("overlay: append: %w", err)
	}
	generation := s.dLog.GetGeneration(logFile)
	if err := index.IndexPatch(s.index, entry, string(logFile), pos, scopeOverlayTx,
		generation, overlay, nil, &scopeID); err != nil {
		return fmt.Errorf("overlay: index: %w", err)
	}
	s.logger.Info("scope overlay written", "scope", scopeID, "commit", commit, "logFile", logFile)
	return nil
}

// latestOverlay returns the newest overlay at or below commit for this scope.
//
// It walks DOWN from commit and stops at the first one, the same way
// findSnapshotBaseReader finds a baseline snapshot, so the work is proportional to what
// has been written since the overlay rather than to the scope's whole history. That only
// works because the overlay carries a patch's segment shape -- see WriteScopeOverlay.
func (s *Storage) latestOverlay(scopeID string, commit int64) *index.LogSegment {
	iter := s.index.IterAtPath("")
	s.index.RLock()
	defer s.index.RUnlock()
	for seg := range iter.CommitsAt(commit, index.Down) {
		if !isOverlaySegment(seg) || *seg.ScopeID != scopeID {
			continue
		}
		if seg.EndCommit > commit {
			continue
		}
		c := seg
		return &c
	}
	return nil
}

// unconditionalPatch rewrites checked replaces into the value they install. An overlay is
// re-applied to a baseline expected to have moved, and !replace{from,to} verifies the
// document still equals from -- against a moved baseline it errors outright rather than
// mis-applying. Plan R1.
func unconditionalPatch(n *ir.Node) *ir.Node {
	if n == nil {
		return nil
	}
	if ir.TagHas(n.Tag, "!replace") {
		if to := ir.Get(n, "to"); to != nil {
			return unconditionalPatch(to.Clone())
		}
	}
	for i, v := range n.Values {
		n.Values[i] = unconditionalPatch(v)
	}
	return n
}
