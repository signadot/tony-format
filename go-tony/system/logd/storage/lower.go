package storage

import (
	"fmt"
	"sync/atomic"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
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

// EnableLowering turns lowering on or off. ON by default; passing false is the escape
// hatch, and with it off a store keeps what a client sent, as it always did.
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
//
// It is not what ships. What ships asks api.NeedsLowering per write and lowers the
// ones that need it, which is what makes the read on the write path free: the state
// verifyApplies already produced is both sides of the diff.
func (s *Storage) LowerEverything(v bool) { s.lowering, s.lowerAll = true, v }

// storableDelta answers how next differs from base, said in the vocabulary a store may
// keep: every operation in it states what a value IS, so applying it to a base that has
// moved gives what it gave here.
//
// It is the one place that turns two states into a delta. There were two, and they
// disagreed about how to be absolute -- this one asks the diff not to make a relative
// primitive, the other let it and rewrote the !replace afterwards, which cannot reach a
// !strdiff or an !arraydiff because an edit script does not carry the value it would
// produce. Every difference between the two copies cost a defect this was found by.
//
// What it does NOT decide is left to the caller, because the two callers genuinely
// differ and saying so is the point:
//
//	presentation  the overlay compares two independent materializations and strips it
//	              first; a write's base and next come from one chain, where a difference
//	              in it is the write's
//	comments      a write delta must carry them, having no other way to say a comment
//	              changed; the overlay re-states every owned path with comments on, so
//	              asking the diff as well is redundant -- and harmful, since a comment
//	              makes a node an operation and the overlay merges values into what it
//	              finds (nm5r3sxah12ks2zmj5n0)
//	marking       a write wants the marker where the change lands, so a narrow read can
//	              skip it; an overlay is a base and wants to be applied to every read
//	validation    identical, and the callers say it differently because the failure
//	              means something different to each
func storableDelta(base, next *ir.Node, keys map[string]string, comments bool) *ir.Node {
	// Stored state is op-free, so diffArray cannot take its keyed branch: it needs
	// !key(f) on BOTH sides. Without this a change to a keyed array comes out
	// POSITIONAL, and a scope storing one takes ownership of the whole array --
	// baseline adds an element and the scope never sees it.
	base, next = base.Clone(), next.Clone()
	annotateKeyed(base, "", keys)
	annotateKeyed(next, "", keys)

	opts := []tony.DiffOpt{tony.DiffAbsolute(true)}
	if comments {
		opts = append(opts, tony.DiffComments(true))
	}
	return tony.DiffWith(base, next, opts...)
}

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
		// An empty document reads back as nil, and null is what the read path's own
		// empty-base branch folds onto.
		base = ir.Null()
	}
	if next == nil {
		// The write removed everything, and a diff of two STATES cannot say that:
		// the absent document is not the null one. Coercing next to null stored "the
		// document is null" for a write that said "there is no document", so a root
		// !delete read back as null where the same write kept as sent read back as
		// nothing (xqpvk3ehh12ks89mj5n0).
		delta := libdiff.MakeDiff(base.Clone(), nil)
		if delta == nil {
			return nil, nil
		}
		markDeltaRoots(delta)
		if err := api.ValidateForStorage(delta); err != nil {
			return nil, fmt.Errorf("lowering %s left something unstorable: %w", op, err)
		}
		return delta, nil
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

	// Presentation is not stripped, unlike the overlay: base and next come from one
	// chain -- next is base with this patch applied -- so a difference in it is this
	// write's. Comments are carried, because a write whose only change is a comment
	// has nothing else to say it with.
	delta := storableDelta(base, next, keys, true)
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
	// Through the comment wrapper, as TagPatchRoots does -- it marks
	// ir.Uncomment(pd.API.Data), and for the same reason. A marker on a wrapper is
	// seen by nothing: the log writes a comment as its lines and its child, so the
	// tag is not serialized, and the entry reaches the read path with no patch root
	// at all. The processor then applies NOTHING from it, which is invisible while
	// the base is empty -- that path folds patches directly -- and loses the whole
	// write once a snapshot is the base (xqpvk3ehh12ks89mj5n0).
	n = ir.Uncomment(n)
	if n == nil {
		return
	}
	// Down to the deepest CONTAINER the change is inside, and mark that -- not the
	// changed field, and not the value under it. An operation on a field is about
	// that field within its container, so the container is what the patch is rooted
	// at: `a.b <- {k0: 9}` from a client is stored as
	//
	//	a: b: !logd-patch-root {k0: 9}
	//
	// and the delta for the same write should be marked in the same place. Marking
	// the leaf instead put the marker at a.b.k0, which is not where the write lands.
	if n.Type == ir.ObjectType && len(n.Fields) == 1 && len(n.Values) == 1 && n.Tag == "" {
		// Not past a COMMENT. A head comment is a wrapper at its own path, and the
		// marker says where the patch is applied FROM: a marker below the wrapper
		// leaves the wrapper outside the subtree that gets applied, so the comment
		// is simply not there on the way back in. The head keeps it, the replay does
		// not, and the two disagree over a comment while every value matches.
		//
		// A client's write cannot reach this: it is rooted at the path it names,
		// which is the path the comment is on.
		if n.Values[0].Type == ir.CommentType {
			tx.MarkPatchRoot(ir.Uncomment(n.Values[0]))
			return
		}
		// An EMPTY container is descended into like any other. It used to require
		// fields, on the reading that a container with none is not passed through --
		// but it is not the statement either, the field holding it is: `{a: {}}` says
		// a is now empty, which is a statement about a and was marked at the root.
		//
		// A scope's delete of a path that does not exist yet produces exactly that
		// shape, so the entry a scope's own write became was marked as a patch on the
		// whole document.
		if sub := ir.Uncomment(n.Values[0]); sub != nil && sub.Type == ir.ObjectType {
			markDeltaRoots(n.Values[0])
			return
		}
	}
	tx.MarkPatchRoot(n)
}

