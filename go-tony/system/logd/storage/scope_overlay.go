package storage

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
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

	// Presentation is how a value was WRITTEN, not what it is, and ir/tags.go names it a
	// category that patching drops first. Two materialized states can differ in it for
	// reasons that are nobody's intent -- one side reconstructed from a snapshot, the
	// other from patch replay -- and a diff over that difference emits a tag op, which an
	// overlay then re-asserts against a document that never had the tag. Strip it from
	// both before comparing, so the overlay describes data.
	keys := s.keyedArrayPaths()
	annBase, annScoped := stripPresentationDeepIR(base.Clone()), stripPresentationDeepIR(scoped.Clone())
	annotateKeyed(annBase, "", keys)
	annotateKeyed(annScoped, "", keys)
	overlay := unconditionalPatch(tony.Diff(annBase, annScoped))

	// A minimal diff records only where the two states differ, so a scope that wrote the
	// value baseline already holds records nothing and loses the path. The index knows
	// what it wrote; re-state each of those from the scoped view. Plan R3.
	for _, p := range s.scopeOwnedLeafPaths(scopeID) {
		v, err := scoped.GetKPath(p)
		if err != nil || v == nil {
			continue // absent in the scoped view: the diff's tombstone is what holds it
		}
		// A keyed element cannot be rooted at items("A"): RootPatchAt builds from a kpath,
		// and a key segment carries the key VALUE while constructing the patch needs the
		// key FIELD. Root at the array with a one-element keyed list, so the assertion
		// merges by identity and leaves every element baseline owns alone.
		root, val := p, v.Clone()
		if arr, field, keyed := splitKeyedElemPath(p, keys); keyed {
			list := ir.FromSlice([]*ir.Node{val})
			list.Tag = ir.TagCompose("!key", []string{field}, "")
			root, val = arr, list
		}
		rooted, err := tx.RootPatchAt(root, val)
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
		p := seg.KindedPath
		if p == "" {
			continue
		}
		// A keyed ELEMENT is the ownership unit, not the fields inside it: items("G").q
		// cannot be rooted on its own, since RootPatchAt has no way to build the keyed
		// list a key segment implies. Truncate to the deepest element.
		if i := strings.LastIndexByte(p, ')'); i >= 0 {
			p = p[:i+1]
		}
		seen[p] = true
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

// hasDescendant reports whether any recorded path lies beneath p. A key segment is a
// descendant marker just as a dot is: items("G") is beneath items. Testing only for "."
// left items itself looking like a leaf, so the overlay re-stated the WHOLE array as the
// scope's and froze baseline out of it.
func hasDescendant(p string, all map[string]bool) bool {
	for q := range all {
		if q != p && (strings.HasPrefix(q, p+".") || strings.HasPrefix(q, p+"(")) {
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
	if s.scopeHasKeyedPaths(scopeID) {
		s.logger.Warn("scope has keyed paths; no overlay written, reads keep replaying",
			"scope", scopeID, "commit", commit)
		return nil
	}
	overlay, err := s.BuildScopeOverlay(scopeID, commit)
	if err != nil {
		return err
	}
	if overlay == nil {
		return nil // the scope holds nothing of its own
	}

	// An overlay is the first thing logd stores that came out of lowering, so it is the
	// first thing that can be held to the storage vocabulary. Failing here is right: an
	// overlay that cannot be stored is one a read would apply wrongly, and the replay path
	// it falls back to is correct.
	if err := api.ValidateForStorage(overlay); err != nil {
		return fmt.Errorf("overlay for scope %q at %d is not storable: %w", scopeID, commit, err)
	}

	// Mark the root the way the commit path marks every stored patch (tx.TagPatchRoots,
	// before MergePatches). The streaming processor finds what to apply by walking for
	// !logd-patch-root, so an entry without it contributes NOTHING when the base is a
	// snapshot -- and contributes normally when the base is empty, because that path
	// folds patches directly instead. An untagged overlay therefore looks correct until
	// the first snapshot exists, which is exactly when it is supposed to start earning
	// its keep.
	overlay.Tag = ir.TagCompose(tx.PatchRootTag, nil, overlay.Tag)

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

// liveScopes lists every scope the index holds data for. P4 in the plan: there is no
// other way to ask -- activeScopes was excised with the old scope-snapshot code and
// DeleteScope is the only lifecycle signal, so a scope exists exactly as long as its
// segments do.
func (s *Storage) liveScopes() []string {
	seen := map[string]bool{}
	for _, seg := range s.index.AllSegments() {
		if seg.ScopeID != nil {
			seen[*seg.ScopeID] = true
		}
	}
	out := make([]string, 0, len(seen))
	for sc := range seen {
		out = append(out, sc)
	}
	sort.Strings(out)
	return out
}

// scopeNeedsOverlay reports whether the scope has written anything the newest overlay does
// not already subsume. Without this every snapshot writes a fresh overlay for every scope
// forever, including scopes nothing has touched since the last one -- which is how a fix
// for unbounded growth becomes a source of it.
func (s *Storage) scopeNeedsOverlay(scopeID string, commit int64) bool {
	ov := s.latestOverlay(scopeID, commit)
	if ov == nil {
		return true // never had one; the scope has segments or liveScopes would not name it
	}
	if ov.EndCommit >= commit {
		return false
	}
	from := ov.EndCommit
	for _, seg := range s.index.LookupRange("", &from, &commit, &scopeID) {
		if seg.ScopeID == nil || *seg.ScopeID != scopeID || isOverlaySegment(seg) {
			continue
		}
		if seg.EndCommit > ov.EndCommit {
			return true
		}
	}
	return false
}

// writeScopeOverlays refreshes every live scope's overlay at commit. Best effort, like
// compaction in SwitchDLog: an overlay is an optimisation over a replay path that still
// works, so failing to write one must not fail the snapshot that carries baseline's.
func (s *Storage) writeScopeOverlays(commit int64) {
	for _, sc := range s.liveScopes() {
		if !s.scopeNeedsOverlay(sc, commit) {
			continue
		}
		if err := s.WriteScopeOverlay(sc, commit); err != nil {
			s.logger.Error("failed to write scope overlay; the scope keeps replaying its patches",
				"scope", sc, "commit", commit, "error", err)
		}
	}
}

// scopeHasKeyedPaths reports whether any path the scope has written contains a kinded KEY
// segment -- items("G") rather than items[0] or items.field.
//
// The spike cannot serve those correctly and must not pretend to. Two independent reasons,
// both tracing to the same missing fact -- nothing tells the overlay builder what keys an
// array (plan P1):
//
//  1. Ownership granularity. A key segment is not a DOTTED descendant, so items("G").q
//     does not make items an ancestor by the prefix test below, and items survives as an
//     owned "leaf". The overlay then re-states the whole array as the scope's, freezing
//     baseline out of it: baseline adds an element and the scope never sees it; baseline
//     updates its own element and the scope keeps the old value. Measured, and silent.
//  2. Even with ownership at element granularity, Diff over op-free state cannot take its
//     keyed branch (plan R2/3.5), so the overlay comes out positional and lands the
//     scope's elements by index.
//
// Falling back to the replay path is correct, just slow -- which is the right way round.
func (s *Storage) scopeHasKeyedPaths(scopeID string) bool {
	keys := s.keyedArrayPaths()
	for _, seg := range s.index.AllSegments() {
		if seg.ScopeID == nil || *seg.ScopeID != scopeID || isOverlaySegment(seg) {
			continue
		}
		p := seg.KindedPath
		if !strings.ContainsRune(p, '(') {
			continue
		}
		// Any keyed path, declared or not, still falls back to replay -- see the note
		// below. keys is consulted only so the reason can be logged accurately.
		_ = keys
		return true
	}
	return false
}

// Why a SCHEMA-DECLARED keyed path still falls back, even though the overlay can now be
// annotated from that schema and produces a correct identity-based diff:
//
// the schema keys the INDEX, not the MERGE. schemaForScope is handed to IndexPatch, and
// nothing puts !key(f) into the patch itself, so a write of {items: [{sku: "G"}]} against
// a baseline holding sku W merges POSITIONALLY -- W is replaced, not merged -- while the
// index records items("G"). Annotating the overlay therefore makes it identity-based while
// the replay it is checked against stays positional, and the two disagree by construction:
// the overlay keeps baseline's other elements and the replay does not.
//
// The overlay is arguably the answer the declaration asked for. It is not the answer the
// store gives. Making them agree means deciding the question issue hbn7ptxch12krs778smg
// left open and 5hmq80f3h12krh1mbsn0 inherited: whether logd INJECTS !key(f) into a write
// whose array the schema declares keyed, or REJECTS a write that omits it. Until then a
// keyed scope replays, which is slow and right.

// keyedArrayPaths is the schema's keying as the overlay builder needs it: array path ->
// key field, over both declarations, since auto-id is keying that also generates.
func (s *Storage) keyedArrayPaths() map[string]string {
	sch := s.schemaForScope(nil)
	if sch == nil {
		return nil
	}
	out := map[string]string{}
	for _, f := range sch.AutoIDFields {
		out[f.Path] = f.Field
	}
	for _, f := range sch.KeyFields {
		out[f.Path] = f.Field
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// annotateKeyed tags arrays at the schema's keyed paths with !key(f), in place.
//
// Stored state is op-free, so diffArray cannot take its keyed branch: it needs !key(f) on
// BOTH sides. Without this the overlay comes out POSITIONAL and lands the scope's elements
// by index -- and re-asserted on every read, a positional diff duplicates them. Annotating
// is legitimate exactly where storing the tag is not: the overlay is a WRITE, and a write
// is where ops belong.
//
// Elements inherit the array's path, matching api.AutoIDField.Path ("orders.items") and
// indexPatchRec's own recursion. Node.GetPath returns a Clone, so this tags on the way
// down rather than by lookup.
func annotateKeyed(n *ir.Node, prefix string, keys map[string]string) {
	if n == nil {
		return
	}
	if f, ok := keys[prefix]; ok && n.Type == ir.ArrayType {
		if _, args := ir.TagGet(n.Tag, "!key"); len(args) != 1 {
			n.Tag = ir.TagCompose("!key", []string{f}, n.Tag)
		}
	}
	switch n.Type {
	case ir.ObjectType:
		for i, fld := range n.Fields {
			p := fld.String
			if prefix != "" {
				p = prefix + "." + fld.String
			}
			if i < len(n.Values) {
				annotateKeyed(n.Values[i], p, keys)
			}
		}
	case ir.ArrayType:
		for _, v := range n.Values {
			annotateKeyed(v, prefix, keys)
		}
	}
}

// splitKeyedElemPath reports the array path and key field when p names a keyed ELEMENT --
// items("A") rather than items or items.field.
func splitKeyedElemPath(p string, keys map[string]string) (arrayPath, keyField string, ok bool) {
	if len(p) == 0 || p[len(p)-1] != ')' {
		return "", "", false
	}
	open := strings.LastIndexByte(p, '(')
	if open <= 0 {
		return "", "", false
	}
	arrayPath = p[:open]
	f, ok := keys[arrayPath]
	return arrayPath, f, ok
}

// stripPresentationDeepIR removes presentation tags throughout, in place.
func stripPresentationDeepIR(n *ir.Node) *ir.Node {
	if n == nil {
		return nil
	}
	n.Tag = ir.StripPresentation(n.Tag)
	for _, f := range n.Fields {
		stripPresentationDeepIR(f)
	}
	for _, v := range n.Values {
		stripPresentationDeepIR(v)
	}
	return n
}
