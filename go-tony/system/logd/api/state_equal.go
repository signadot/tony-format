package api

import "github.com/signadot/tony-format/go-tony/ir"

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
