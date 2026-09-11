package storage

import (
	"errors"
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
//   - The read a lowering needs is already taken. lowerWrite reads the state at each
//     site the write names and applies the write to it, on every write, to refuse a
//     delta the store cannot apply. Both sides of the diff are then in hand, so a
//     lowered write costs a diff and no further read.
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
var loweringFired, loweringSkipped int64

// lowerEverything lowers every write, whether or not it needs it. It cannot change what
// a watcher receives -- the notification is the stored delta either way -- only whether
// an absolute write is diffed or kept as sent, which is the optimisation NeedsLowering is.
//
// Not a mode to run in: it pays a diff on writes that were already their own delta, and
// it stores a delta where the client's own patch would have been kept, so a client
// reading its write back does not see the shape it sent.
//
// It is how the lowering is TESTED. With the ordinary rule the suite exercises the path
// a handful of times in tens of thousands of writes, because nearly nothing anyone
// writes is relative -- which is the point, and which also means a green suite says
// almost nothing about whether lowering is correct. Forcing it puts every state
// transition the suite produces through DiffAbsolute and back.
func (s *Storage) lowerEverything(v bool) { s.lowerAll = v }

// storableDelta answers how next differs from base, said in the vocabulary a store may
// keep: every operation in it states what a value IS, so applying it to a base that has
// moved gives what it gave here.
//
// It is the ONE place that turns two states into a delta, and being one place is the
// point: there were two, and they disagreed about how to be absolute -- this one asks the
// diff not to make a relative primitive, the other let it and rewrote the !replace
// afterwards, which cannot reach a !strdiff or an !arraydiff because an edit script does
// not carry the value it would produce. Every difference between the two copies cost a
// defect it was found by.
//
// Base and next come from ONE chain: next is base with this write applied. Everything
// here follows from that.
//
//	presentation  carried, not stripped. A difference in how a value is written is this
//	              write's doing, because nothing else touched it. (Two states
//	              materialized independently are a different situation and want the
//	              strip; no caller here is in it.)
//	comments      carried. A write whose only change is a comment has no other way to
//	              say so.
//	validation    left to the caller, because what a failure means differs: an
//	              unstorable write is refused, and an unstorable anything-else is a bug.
func storableDelta(base, next *ir.Node) *ir.Node {
	// A keyed array is an object of names in both states (tx.LowerKeyed), so the diff is
	// an object diff and needs nothing said about keys: a changed element is a changed
	// field, and the others are not mentioned.
	return tony.DiffWith(base, next, tony.DiffAbsolute(true), tony.DiffComments(true))
}

// WriteBudgetError is a write, or a precondition, refused because verifying it would hold
// more than the store's write budget: the value at Path is larger than Budget bytes.
//
// It is the bound charging the client that asked for it. An operation's site is the node
// it is written on, and some sites are large by construction -- a !replace of a whole
// subtree, an !all over every element -- so the intermediate a write needs is what that
// write installs. The remedy is in the client's hands: write narrower, or write the change
// as absolute values at the paths that actually change, which needs no wide read at all.
//
// The remedy the STORE could offer, and someday will want to, is a patch exploder: one
// write of !all at a container becomes one write per element, each within the budget,
// committed as one transaction. That turns the one operation whose site is the whole
// container into N whose sites are the elements, and is the natural continuation of
// per-path lowering rather than an exception to it.
type WriteBudgetError struct {
	Path   string
	Budget int64
	Op     string
}

func (e *WriteBudgetError) Error() string {
	what := "verifying the write"
	if e.Op != "" {
		what = "lowering " + e.Op
	}
	return fmt.Sprintf("write at %s refused: %s needs more than the %d-byte write budget; "+
		"write the change narrower, or as absolute values at the paths that change",
		pathOrRoot(e.Path), what, e.Budget)
}

func (e *WriteBudgetError) Unwrap() error { return ErrBudget }

func pathOrRoot(kp string) string {
	if kp == "" {
		return "the document root"
	}
	return fmt.Sprintf("%q", kp)
}

// lowerWrite verifies a write at every path it names and answers the delta the log should
// keep for it: nil, with no error, when the write changed nothing.
//
// merged is the write as the client sent it, rooted at the document, which is what an
// absolute write is stored as. sites are the paths the write states something at
// (LowerSites for baseline, ClaimPaths for a scope): the node an operation is written
// on, or a leaf, or an array a position reaches into.
//
// A WRITE IS A RECORD AT A PATH, NOT A DOCUMENT APPLIED TO A DOCUMENT. At each site the
// current value is read -- one bounded read, under the write budget -- the write's node
// for that site is applied to it, and for a write that needs lowering the two are diffed
// there. What is resident is the sites' values and their results, and for a plain merge at
// a deep path that is the leaf it merges into; for a !replace of a subtree it is the
// subtree, which is what that write installs and the intermediate the bound admits.
//
// Baseline stores the DIFFERENCE; a scope stores its CLAIM. A scope's patches replay over
// a baseline that moves, so a scope write is a standing claim -- what the scope holds at
// that path, whatever baseline does afterwards -- and a diff is the effect against one
// baseline, which where it is smaller than the claim loses the claim: a !delete of a path
// baseline has not created yet changes only the spine. An ABSOLUTE scope write needs none
// of this and is stored as sent; only a relative one is converted, and a relative
// operation's meaning depends on the whole subtree it was applied to, so the subtree is
// what it claims (claimValue).
func (s *Storage) lowerWrite(commit int64, merged *ir.Node, scopeID *string, sites []string) (*ir.Node, error) {
	if merged == nil {
		return merged, nil
	}
	scoped := scopeID != nil
	op, needs := api.NeedsLowering(merged)

	type site struct {
		path       string
		base, next *ir.Node
	}
	verified := make([]site, 0, len(sites))
	for _, p := range sites {
		base, err := s.stateAt(commit-1, scopeID, p)
		if err != nil {
			var wb *WriteBudgetError
			if errors.As(err, &wb) {
				wb.Op = op
				return nil, wb
			}
			return nil, fmt.Errorf("cannot read the state at %d to check the patch: %w", commit-1, err)
		}
		at, err := merged.GetKPathWith(p, ir.WithComments(true))
		if err != nil || at == nil {
			continue // the write states nothing at this site after all
		}
		if base == nil {
			// An empty path reads back as nil, and null is what the read path's own
			// empty-base branch folds onto.
			base = ir.Null()
		}
		next, err := api.NextState(base, at)
		if err != nil {
			return nil, &api.DoesNotApplyError{Commit: commit, Err: err}
		}
		verified = append(verified, site{path: p, base: base, next: next})
	}

	if !needs && (!s.lowerAll || scoped) {
		// Nothing to do, and for a SCOPE that is not a shortcut: an absolute patch is
		// already the claim a scope stores, so forcing it through the claim would replace
		// "what the client said" with "the subtree it landed in", taking baseline's
		// siblings into the scope's ownership.
		atomic.AddInt64(&loweringSkipped, 1)
		return merged, nil
	}
	if scoped && len(verified) == 0 {
		// Nothing names what is being claimed: an unattributable write must not
		// silently claim the root.
		atomic.AddInt64(&loweringSkipped, 1)
		return merged, nil
	}
	atomic.AddInt64(&loweringFired, 1)

	// The delta, site by site, the pieces rooted together into one patch -- the same
	// construction MergePatches gives a client's own multi-participant write. Where each
	// piece lands is in the shape: a read below one site sees nothing of another.
	pds := make([]*tx.PatcherData, 0, len(verified))
	for _, v := range verified {
		var delta *ir.Node
		switch {
		case scoped:
			if v.next == nil {
				// The write left nothing there, and "nothing" is as much a claim as a
				// value: without it a later baseline write at that path shows through.
				delta = ir.Null().WithTag(libdiff.DeleteTag)
			} else {
				delta = claimValue(v.next.Clone())
			}
		case v.next == nil:
			// The write removed everything here, and a diff of two STATES cannot say
			// that: the absent value is not the null one (xqpvk3ehh12ks89mj5n0).
			delta = libdiff.MakeDiff(v.base.Clone(), nil)
		default:
			delta = storableDelta(v.base, v.next)
		}
		if delta == nil {
			continue // nothing changed at this site
		}
		pds = append(pds, &tx.PatcherData{API: &api.Patch{PathData: api.PathData{Path: v.path, Data: delta}}})
	}
	if len(pds) == 0 {
		return nil, nil
	}
	out, err := tx.MergePatches(pds)
	if err != nil {
		return nil, fmt.Errorf("lowering %s: %w", op, err)
	}
	// Held to the vocabulary it was lowered into. Failing here is right: a delta the
	// store cannot promise to re-apply is worse than a refused write, because the
	// client can retry a refusal and cannot repair a stored fault.
	if err := api.ValidateForStorage(out); err != nil {
		return nil, fmt.Errorf("lowering %s left something unstorable: %w", op, err)
	}
	return out, nil
}

// claimValue is a value stated as the whole of what is at its path: !insert.raw over it.
//
// A claim says two things, and they are two operations. !insert applies its child
// against absence and answers with the result whatever was there, so the subtree is
// exactly this: without it the claim is an ordinary merge patch and a CONTAINER only
// merges -- a scope claiming `a: {y: 1}` over a baseline `a: {x: 1}` read back
// `{x: 1, y: 1}`, so a !rename in a scope left the old field standing. !raw says nothing
// inside it is an instruction: the value came out of a document, where an operation tag
// is data (6225etzfh12kr955fxn0), and the escape alone MERGES (mergeop/raw.go), which is
// why it is not enough on its own. The chain is the one a diff already emits for a value
// that holds an operation (libdiff.escaped), and under the cover rules it is a TOTAL
// cover of its path (scope_compaction.go), which is what lets a later claim retire every
// earlier statement beneath it.
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
	return n.WithTag(ir.TagCompose(libdiff.InsertTag, nil, ir.TagCompose(libdiff.RawTag, nil, n.Tag)))
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
//	too. A keyed element -- votes."(id=a)" -- is an identity, and is claimed as itself.
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
		// Through the comment wrapper, as the patch index does:
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
		kids := index.PatchChildren(n, at)
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

// LowerSites answers the paths a BASELINE write is verified and lowered at. They are
// ClaimPaths' sites -- the node an operation is written on, or a leaf, or the array a
// position reaches into -- with one difference: a node wearing a head comment is a site
// itself, and the walk stops there.
//
// A scope's claim descends through the comment to the leaf on purpose, because claiming the
// container would freeze it (ClaimPaths). Baseline stores a DIFFERENCE, and the comment is
// something the write states at that node: a diff taken below it, at the leaf, sees the
// leaf's change and never the comment above it, and the comment is lost from the log.
// Taking the site at the commented node costs the read of that node, which is what a
// write to it is.
func LowerSites(path string, data *ir.Node) []string {
	var out []string
	seen := map[string]bool{}
	claim := func(p string) {
		p = aboveAnyIndex(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	var walk func(n *ir.Node, at string)
	walk = func(n *ir.Node, at string) {
		if n == nil {
			return
		}
		if n.Type == ir.CommentType {
			claim(at)
			return
		}
		if _, op, _, _, err := mergeop.SplitChild(n); err == nil && op != "" {
			claim(at)
			return
		}
		kids := index.PatchChildren(n, at)
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
