package api

import (
	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// Presence, and what a nil is.
//
//	A nil *ir.Node is ABSENT. Null is ir.Null(). No layer maps one onto the other.
//
// (nil, nil) is how a read says "there is nothing here", and it is the right spelling: it
// is what GetPath answers for a missing field, what mergeop's keyed merge documents as
// deliberate, and what lowering relies on to say "the document is gone" -- a diff of two
// STATES cannot say that with a null, because a null is a state. The rule is stated here
// rather than at each layer because a layer that turns an absent path into ir.Null() so
// two things can be compared has made a null that is then deleted look like no change at
// all, and the gate downstream of it tells the truth only if nothing upstream lied.
//
// The wire keeps them apart too: a watch event says absent with WatchEvent.Absent, and a
// read of a path holding nothing is answered not_found (presence.md, wk5w1ddkh12krj1tkxn0).

// NextState applies a patch the way logd materializes state: keeping comments,
// because a store keeps what it is given.
//
// tony.Patch strips comments unless asked not to, which is right for a caller
// that wants data and wrong for a store: applied at eleven places -- every head
// step, every watch step, every read, every snapshot build -- it meant a comment
// could not survive being written even once. That was the second of two gates.
// The first was the stream decoder, which dropped comment tokens before they
// reached any of this.
//
// The alternative was a flag, and the flag was worse. A store's comment policy
// would then be a property of the PROCESS rather than of the data: two peers on
// one directory could disagree, and a restart with it off did not merely hide
// comments, it lost them a subtree at a time -- the snapshot builder forwards
// untouched base events verbatim, so comments survived where nobody wrote and
// vanished where anybody did. Turning it back on would return half a document,
// which is worse than none, because it looks like it worked.
//
// So there is no policy to get wrong: comments go in and come back. A caller
// that does not want them strips them, which is one call and cannot be applied
// to somebody else's data by accident (3cdjz00jh12krns4g1n0).
// It also refuses an operation which calls out to the system. mergeop has had
// RejectUnsafe since before logd stored anything and nothing set it, so a stored
// !pipe ran -- on every read, every replay, every snapshot build. That made the
// same commit read two ways: a store holds values, and an operation which
// re-evaluates is not one (trqgmd1ah12kranxg5n0). It is set HERE because this is
// the one place logd applies a patch, so no path is left able to execute one; the
// write side refuses it too, so a store does not have to fail reads to enforce it.
func NextState(doc, patch *ir.Node) (*ir.Node, error) {
	return tony.Patch(doc, patch, mergeop.Comments(true), mergeop.RejectUnsafe(true))
}

// StepState is NextState for a caller stepping its own state forward, which keeps only
// the answer: a watch's value, a fold of log records. It takes doc over
// (tony.PatchOwned) rather than copying it, so a step costs what the patch touches and not
// the size of the state -- a watcher on a set of three thousand entities paid a copy of
// the set per commit otherwise. doc may be compared with the answer afterwards, and must
// not be used for anything else.
func StepState(doc, patch *ir.Node) (*ir.Node, error) {
	return tony.PatchOwned(doc, patch, mergeop.Comments(true), mergeop.RejectUnsafe(true))
}

// SameState reports whether two documents are the same STATE: what the store
// holds at a path, as the store holds it.
//
// It is the one place logd decides what counts as a change. A watch asks the
// question at every step -- stepping the value it holds by a delta, replayed or
// live, and re-reading its path in a scoped view -- and asking it in more than one
// place with more than one answer is how the answers drift apart.
//
// The answer counts comments. Not because comments are important, but because
// this asks what the STORE holds, and it is not this function's business to
// decide that some of it does not count: ir.Node.DeepEqual is the right question
// about two VALUES, and deliberately blind (see its comment), while what a watch
// owes its watcher is everything the commit changed.
//
// A store keeps comments (NextState), so blind equality would put the store and
// its watchers into disagreement about whether anything happened: the log holds a
// commit that every watch dropped. So the policy is chosen once, here
// (3cdjz00jh12krns4g1n0).
func SameState(a, b *ir.Node) bool {
	return a.DeepEqualWithComments(b)
}

// WireOptions is how a session encodes a message: the compact wire form, and
// comments with it.
//
// Both halves of every hop have to agree, and there is an encoder at every hop
// across logd, docd, libctl and the transaction pool. A convention copied into
// each is how a message loses something at one hop and nobody can say which -- so
// the convention is written once, here, beside the two other things a store's
// treatment of state is decided by (NextState and SameState).
//
// Comments make a wire message multi-line, which is safe everywhere it is used:
// every peer reads with stream.ReadDocument, which ends a document where its
// structure closes, and the dlog frames entries by length. Nothing frames by
// line (3cdjz00jh12krns4g1n0).
func WireOptions() []gomap.MapOption {
	return []gomap.MapOption{gomap.EncodeWire(true), gomap.EncodeComments(true)}
}
