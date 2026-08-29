package storage

import (
	"fmt"
	"sync/atomic"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
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

// claimDelta answers what a SCOPE stores for a write: the claim it is making, rather
// than the difference it made.
//
// A scope's patches replay over a baseline that moves, so a scope write is a standing
// claim -- what the scope holds at that path, whatever baseline does afterwards. A
// diff is the effect against one baseline, and where the effect is smaller than the
// claim the claim is lost: a !delete of a path baseline has not created yet changes
// only the spine, so the delta says `a: {}`, which merges to nothing, and the scope
// stops shadowing that path forever after.
//
// An ABSOLUTE write needs none of this. It is already a claim -- what it says is what
// results, whatever it lands on -- and it is stored as the client sent it. Only a
// relative write has to be converted, and a relative operation's meaning depends on
// the whole subtree it was applied to, so the subtree is what it claims.
//
// So: baseline stores differences, a scope stores claims, and the two layers lower
// differently because they own different things.
//
// The claim is BUILT as a patch and never applied while it is being built. A claim has
// to be able to say that a path holds nothing -- that is the whole reason it exists --
// and only a patch can say that. A document cannot: absence in one is indistinguishable
// from a path nobody mentioned. So the claims are collected as (path, statement) pairs
// and rooted together into one patch by tx.MergePatches.
//
// Accumulating them with api.NextState instead put each claim in the INSTRUCTION role
// against the claim-so-far, which is a patch standing in the document position. A value
// survives that, since a value means the same thing in both roles; a tombstone does
// not. It ran against the half-built claim and left only its effect there, so
//
//	d <- {k1: !replace {from: 1, to: 5}, k0: !delete}
//
// stored a claim that never mentioned k0, and the scope read k0 back -- the client's
// delete lost at the write itself, with the field ORDER deciding it: claimed after k1
// the tombstone was applied and vanished, claimed before it, it survived as data.
//
// MergePatches also refuses two paths where one contains the other, which ClaimPaths
// should never produce; if it ever does, that is an error rather than one claim
// quietly overwriting another.
func claimDelta(next *ir.Node, paths []string) (*ir.Node, error) {
	pds := make([]*tx.PatcherData, 0, len(paths))
	for _, p := range paths {
		var stmt *ir.Node
		held, err := next.GetKPathWith(p, ir.WithComments(true))
		switch {
		case err != nil || held == nil:
			// The write left nothing there, and "nothing" is as much a claim as a
			// value: without it a later baseline write at that path shows through.
			stmt = ir.Null().WithTag(libdiff.DeleteTag)
		default:
			stmt = claimValue(held.Clone())
		}
		pds = append(pds, &tx.PatcherData{
			API: &api.Patch{PathData: api.PathData{Path: p, Data: stmt}},
		})
	}
	out, err := tx.MergePatches(pds)
	if err != nil {
		return nil, fmt.Errorf("claiming %v: %w", paths, err)
	}
	return out, nil
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
func (s *Storage) lowerWrite(base, next, merged *ir.Node, scoped bool, paths []string) (*ir.Node, error) {
	if !s.lowering || merged == nil {
		return merged, nil
	}
	// The root tags are logd's own marker, not an operation, and the question is
	// about the client's patch. Ask the deliverable copy, which is what the
	// notification carries and what verifyApplies was given.
	op, needs := api.NeedsLowering(DeliverablePatch(merged))
	if !needs && (!s.lowerAll || scoped) {
		// Nothing to do, and for a SCOPE that is not a shortcut: an absolute patch
		// is already the claim a scope stores, so forcing it through claimDelta
		// would replace "what the client said" with "the subtree it landed in",
		// taking baseline's siblings into the scope's ownership. LowerEverything
		// cannot ask for that, because there is nothing there to lower.
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

	// What the log will keep: a scope's claim or baseline's difference. Whichever it
	// is, it leaves by the same door below -- both are stored deltas and the rules
	// that make a stored delta readable are not about which one it is.
	var delta *ir.Node
	switch {
	case scoped && len(paths) == 0:
		// Nothing names what is being claimed. Keeping the patch as sent is what the
		// store did before lowering existed, and is right here for the same reason:
		// an unattributable write must not silently claim the root.
		atomic.AddInt64(&loweringSkipped, 1)
		return merged, nil

	case scoped:
		// A scope claims; baseline differs. Only a RELATIVE write gets here for a
		// scope -- an absolute one was kept as sent above, being a claim already.
		// See claimDelta.
		var err error
		if delta, err = claimDelta(next, paths); err != nil {
			return nil, err
		}

	default:
		// Presentation is not stripped, unlike the overlay: base and next come from
		// one chain -- next is base with this patch applied -- so a difference in it
		// is this write's. Comments are carried, because a write whose only change is
		// a comment has nothing else to say it with.
		delta = storableDelta(base, next, keys, true)
	}
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

// claimValue is a value stated as the whole of what is at its path.
//
// !raw answers with its child and never looks at the document, so it says both halves
// of what a claim needs at once: the subtree is exactly this, and nothing inside it is
// an instruction. Without it the claim is an ordinary merge patch and a CONTAINER only
// merges -- a scope claiming `a: {y: 1}` over a baseline `a: {x: 1}` read back
// `{x: 1, y: 1}`, so a !rename in a scope left the old field standing.
//
// The value came out of a document, where an operation tag is data, and !raw is what
// says so (6225etzfh12kr955fxn0) -- the same reason libdiff.Escape reaches for it.
//
// A head comment is a WRAPPER around the value, and an operation belongs on the value:
// a tag on the wrapper is seen by nothing, since mergeop walks past a comment before it
// looks for an operation (xqpvk3ehh12ks89mj5n0).
func claimValue(n *ir.Node) *ir.Node {
	if n.Type == ir.CommentType && len(n.Values) == 1 {
		n.Values[0] = claimValue(n.Values[0])
		n.Values[0].Parent = n
		n.Values[0].ParentIndex = 0
		return n
	}
	return n.WithTag(ir.TagCompose(libdiff.RawTag, nil, n.Tag))
}

// ClaimPaths answers the paths a scoped write CLAIMS: where the patch states
// something, rather than where the client happened to root it.
//
// A client writing at the root sends `{a: {x: !replace {...}}}`, and claiming the root
// for it would freeze the scope's whole document -- everything baseline did afterwards,
// anywhere, would stop showing through. The patch says where its parts land, so the
// claim follows them down. It is a set and not a path because a patch may state more
// than one thing: `{a: 1, b: !replace {...}}` claims a and b, and neither claims the
// container they share.
//
// Where the parts land is index.PatchChildren, which is the one reading of that
// question -- the same one the patch index walks to decide what a narrow read may
// skip. Deriving it again here is how a claim came to be described by a string trim,
// a flow-style special case and a spelling rule for field names, none of which knew
// what a sparse index or a keyed element was.
//
// The descent stops at two things:
//
//	an OPERATION, because what it meant is about the node wearing it. Its operand is
//	not descended into -- `!replace {from: {p: 1}, to: {p: 2}}` at a.x claims a.x, not
//	a.x.p, since what the operation consulted was the whole of a.x.
//
//	a POSITION, because a position is not an identity. An element claimed at votes[1]
//	is claimed as "the second of whatever is there", so the array is what such a write
//	can name, and claiming it is honest: a scope writing by position owns the order
//	too. A keyed element -- votes("a") -- is an identity, and is claimed as itself.
func ClaimPaths(path string, data *ir.Node) []string {
	var out []string
	seen := map[string]bool{}
	claim := func(p string) {
		// Nothing BELOW a position can be named, so a claim reaching through one is
		// made at the array instead. It has to be done to what the descent produces
		// rather than to the path it starts from: votes[1] <- {choice: approve} is
		// about a field of the ELEMENT, and trimming first turned choice into a field
		// of the array and claimed votes.choice, which claimed the array as an object
		// and emptied it.
		p = aboveAnyIndex(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	var walk func(n *ir.Node, at string)
	walk = func(n *ir.Node, at string) {
		// Through the comment wrapper, as markDeltaRoots and the patch index both do:
		// a head comment is not a kind of container, so asking what kind of node this
		// is stops at the wrapper, and `# note` above `{k2: 5}` claimed the whole
		// container the write landed in rather than the leaf it named.
		n = ir.Uncomment(n)
		if n == nil {
			return
		}
		if _, op, _, _, err := mergeop.SplitChild(n); err == nil && op != "" {
			claim(at)
			return
		}
		kids := index.PatchChildren(n, at, nil)
		if len(kids) == 0 {
			claim(at)
			return
		}
		for _, c := range kids {
			walk(c.Node, c.Path)
		}
	}
	walk(data, path)
	return out
}

// aboveAnyIndex answers the deepest path a claim can name on the way to p, which is
// the array above p's first POSITIONAL segment: votes[1] and votes[1].choice are both
// claimed at votes.
//
// A position is not an identity -- it names the second of whatever is there -- so
// neither an element named that way nor anything inside it can be claimed. The array
// is what such a write can name, and claiming it is honest: a scope writing by
// position owns the order too.
//
// A keyed segment -- votes("a") -- IS an identity, and is named as itself.
func aboveAnyIndex(p string) string {
	kp, err := kpath.Parse(p)
	if err != nil {
		return p
	}
	acc := ""
	for x := kp; x != nil; x = x.Next {
		if x.Index != nil || x.IndexAll {
			return acc
		}
		if x.Field != nil {
			acc = kpath.ChildField(acc, *x.Field)
			continue
		}
		acc += x.SegmentString()
	}
	return p
}
