package storage

import (
	"fmt"
	"sync/atomic"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Lowering: what the log keeps for a write is held to the storage vocabulary.
//
// A patch may be written with whatever expressivity tony offers. What is STORED is
// something absolute -- an operation whose result states what the value IS, so that
// applying it to a base that has moved gives what it gave at the write. The user of
// a store believes they are working with data; an operation that re-evaluates later
// breaks that belief, and a store cannot know what mergeops will exist next year.
//
// Two things make this cheap, and both were already true before it was written:
//
//   - Most writes need nothing. A patch built only from absolute operations is
//     already its own delta, so it is kept as it arrived. api.NeedsLowering asks,
//     and a plain data merge -- which is nearly every write -- answers no.
//
//   - The read a lowering needs is already taken. verifyApplies reads the state the
//     patch applies to and computes the result, on every commit, to refuse a delta
//     the store cannot apply and to step the head. Both sides of the diff are in
//     hand before this is called, so a lowered write costs a diff and no read.
//
// It applies to BASELINE as well as scopes, deliberately. Baseline gets away with
// arbitrary operations today only because its replay is deterministic -- the same
// patches re-apply in the same order to the same predecessor states -- and that is a
// property of the operations people happen to use rather than one the store enforces.
// It also makes the wire form uniform: a watcher sees the same shape whichever layer
// it is watching.
//
// !pipe is not lowered. It is refused at the door (tx.checkUnsafeWrite), because
// lowering it would mean running an arbitrary system call inside commitMu.

// loweringFired and loweringSkipped count what the differential needs to know: that
// a suite run with lowering on actually reached the path, rather than passing because
// nothing in it writes a relative operation.
var loweringFired, loweringSkipped, loweringUndeclaredKey int64

// EnableLowering turns lowering on or off. OFF while the differential below is being
// built out; passing true is what the tests use to compare the two paths.
func (s *Storage) EnableLowering(v bool) { s.lowering = v }

// LowerEverything lowers every write, whether or not it needs it.
//
// Not a mode to run in: it pays a diff on writes that were already their own delta,
// and it stores a delta where the client's own patch would have been kept, so a
// client reading its write back does not see the shape it sent.
//
// It is how the lowering is TESTED. With the ordinary rule the suite exercises the
// path 16 times in 21587 writes, because nearly nothing anyone writes is relative --
// which is the point, and which also means a green suite says almost nothing about
// whether lowering is correct. Forcing it puts every state transition the suite
// produces through DiffAbsolute and back.
func (s *Storage) LowerEverything(v bool) { s.lowering, s.lowerAll = true, v }

// lowerWrite answers the delta the log should keep for this write.
//
// base and next come from verifyApplies -- the state the patch was applied to and
// the result. merged is the patch as it would have been stored, root tags and all.
//
// A nil answer with no error means the write changed nothing, which a diff can say
// and a patch cannot: the caller keeps the patch rather than storing an empty delta,
// so a commit that asserts what is already there still takes a commit number and
// still notifies, exactly as it did before.
func (s *Storage) lowerWrite(base, next, merged *ir.Node) (*ir.Node, error) {
	if !s.lowering || merged == nil {
		return merged, nil
	}
	// The root tags are logd's own marker, not an operation, and the question is
	// about the client's patch. Ask the deliverable copy, which is what the
	// notification carries and what verifyApplies was given.
	op, needs := api.NeedsLowering(DeliverablePatch(merged))
	if !needs && !s.lowerAll {
		atomic.AddInt64(&loweringSkipped, 1)
		return merged, nil
	}
	atomic.AddInt64(&loweringFired, 1)
	if base == nil {
		base = ir.Null()
	}
	if next == nil {
		next = ir.Null()
	}
	// A key the SCHEMA does not declare cannot survive the lowering. Stored state is
	// op-free, so the only thing that can say an array is keyed is the schema; a
	// client's own !key(f) rides in the patch, and lowering replaces the patch. The
	// diff would then come out positional and the write would take ownership of the
	// whole array, shutting baseline out of it.
	//
	// So it is not lowered. Keeping the client's patch is what happens today and is
	// correct -- the same conservative answer scopeHasKeyedPaths gives the overlay,
	// for the same missing fact.
	keys := s.keyedArrayPaths()
	if patchHasUndeclaredKey(DeliverablePatch(merged), "", keys) {
		atomic.AddInt64(&loweringUndeclaredKey, 1)
		return merged, nil
	}

	// Stored state is op-free, so diffArray cannot take its keyed branch: it needs
	// !key(f) on BOTH sides. Without this a change to a keyed array comes out
	// POSITIONAL, and a scope storing one takes ownership of the whole array --
	// baseline adds an element and the scope never sees it. Same annotation the
	// overlay builder makes, for the same reason (scope_overlay.go).
	//
	// Presentation is NOT stripped here, unlike there. The overlay compares two
	// independent materializations, which can differ in presentation for reasons
	// nobody intended; base and next come from one chain -- next is base with this
	// patch applied -- so a presentation difference between them is this write's.
	base, next = base.Clone(), next.Clone()
	annotateKeyed(base, "", keys)
	annotateKeyed(next, "", keys)

	// DiffAbsolute is the whole of it: the diff answers with primitives that state
	// what the value is, so nothing it produces consults the base. Without it a
	// changed string comes back as !strdiff and a positional array as !arraydiff,
	// both relative and neither storable -- the plan expected a post-pass to flatten
	// those, and there is nothing to flatten them FROM: an edit script does not carry
	// the value it would produce. It has to be the diff that does not make one.
	//
	// Comments count: !comment is in the vocabulary and is absolute, so a write whose
	// only change is a comment has a delta like any other.
	delta := tony.DiffWith(base, next,
		tony.DiffComments(true), tony.DiffAbsolute(true))
	if delta == nil {
		return nil, nil
	}
	// The streaming processor finds what to apply by walking for !logd-patch-root,
	// so a delta without it contributes nothing once the base is a snapshot -- and
	// contributes normally while the base is empty, which is how an untagged delta
	// looks correct until the first snapshot exists (scope_overlay.go says the same
	// of overlays).
	//
	// WHERE the marker goes is the selectivity of every narrow read: patches.
	// BuildPatchIndex keys entries by the PATH of each marked node, so a marker at
	// the delta's root makes the entry a patch on the whole document and every
	// narrow read replays it. TagPatchRoots marks each participant's own path for
	// exactly this reason; a diff has no participants, so the equivalent is the
	// paths where the change actually lands.
	markDeltaRoots(delta)
	// Held to the vocabulary it was lowered into. Failing here is right: a delta
	// the store cannot promise to re-apply is worse than a refused write, because
	// the client can retry a refusal and cannot repair a stored fault.
	if err := api.ValidateForStorage(delta); err != nil {
		return nil, fmt.Errorf("lowering %s left something unstorable: %w", op, err)
	}
	return delta, nil
}

// markDeltaRoots marks the shallowest nodes that carry the change, rather than the
// delta's root.
//
// It descends through plain objects, which say only "something below here differs",
// and stops at anything that says something itself: a leaf, an array, or a node
// carrying an operation. A write to verse.meta.rev then marks verse.meta.rev, which
// is where the client's own patch was rooted and what the patch index keys on.
//
// A delta that changed two disjoint places marks both, the way two participants used
// to mark their own roots.
func markDeltaRoots(n *ir.Node) {
	if n == nil {
		return
	}
	if n.Type == ir.ObjectType && len(n.Fields) > 0 && n.Tag == "" {
		for _, v := range n.Values {
			markDeltaRoots(v)
		}
		return
	}
	tx.MarkPatchRoot(n)
}
