package api

import (
	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

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
func NextState(doc, patch *ir.Node) (*ir.Node, error) {
	return tony.Patch(doc, patch, mergeop.Comments(true))
}

// SameState reports whether two documents are the same STATE: what the store
// holds at a path, as the store holds it.
//
// It is the one place logd decides what counts as a change. Four places ask the
// question -- a watch replaying the log, a watch following it live, a scoped
// delta, and the head's agreement with a full read -- and asking it four times in
// four files is how the four answers drift apart.
//
// The answer counts comments. Not because comments are important, but because
// this asks what the STORE holds, and it is not this function's business to
// decide that some of it does not count: ir.DeepEqual is the right question about
// two VALUES, and deliberately blind (see its comment), while what a watch owes
// its watcher is everything the commit changed.
//
// While nothing in a store carries a comment the two are the same function, so
// today this is inert. The day a store keeps them, blind equality would put the
// store and its watchers into disagreement about whether anything happened: the
// log holds a commit that every watch dropped. The same holds for the head, where
// a stepped head that lost a comment a read kept is two materializations
// disagreeing about stored content -- which is exactly what that check exists to
// catch, and dropping the head is the safe answer to it.
//
// So the policy is chosen once, here, and turning comment storage on does not
// have to find these four sites again (3cdjz00jh12krns4g1n0).
func SameState(a, b *ir.Node) bool {
	return a.DeepEqualWithComments(b)
}

// WireOptions is how a session encodes a message: the compact wire form, and
// comments with it.
//
// Both halves of every hop have to agree, and there are nine encoders across
// logd, docd, libctl and the transaction pool. Nine copies of a convention is
// how a message loses something at one hop and nobody can say which -- so the
// convention is written once, here, beside the two other things a store's
// treatment of state is decided by (NextState and SameState).
//
// Comments make a wire message multi-line, which is safe everywhere it is used:
// every peer reads with stream.ReadDocument, which ends a document where its
// structure closes, and the dlog frames entries by length. Nothing frames by
// line (3cdjz00jh12krns4g1n0).
func WireOptions() []gomap.MapOption {
	return []gomap.MapOption{gomap.EncodeWire(true), gomap.EncodeComments(true)}
}
