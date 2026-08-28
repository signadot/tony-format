package storage

import (
	"fmt"

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

// EnableLowering turns lowering on or off. OFF while the differential below is being
// built out; passing true is what the tests use to compare the two paths.
func (s *Storage) EnableLowering(v bool) { s.lowering = v }

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
	if !needs {
		return merged, nil
	}
	if base == nil {
		base = ir.Null()
	}
	if next == nil {
		next = ir.Null()
	}
	// DiffAbsolute is the whole of it: the diff answers with primitives that state
	// what the value is, so nothing it produces consults the base. Without it a
	// changed string comes back as !strdiff and a positional array as !arraydiff,
	// both relative and neither storable -- the plan expected a post-pass to flatten
	// those, and there is nothing to flatten them FROM: an edit script does not carry
	// the value it would produce. It has to be the diff that does not make one.
	//
	// Comments count: !comment is in the vocabulary and is absolute, so a write whose
	// only change is a comment has a delta like any other.
	delta := tony.DiffWith(base.Clone(), next.Clone(),
		tony.DiffComments(true), tony.DiffAbsolute(true))
	if delta == nil {
		return nil, nil
	}
	// The streaming processor finds what to apply by walking for !logd-patch-root,
	// so a delta without it contributes nothing once the base is a snapshot -- and
	// contributes normally while the base is empty, which is how an untagged delta
	// looks correct until the first snapshot exists (scope_overlay.go says the same
	// of overlays).
	tx.MarkPatchRoot(delta)
	// Held to the vocabulary it was lowered into. Failing here is right: a delta
	// the store cannot promise to re-apply is worse than a refused write, because
	// the client can retry a refusal and cannot repair a stored fault.
	if err := api.ValidateForStorage(delta); err != nil {
		return nil, fmt.Errorf("lowering %s left something unstorable: %w", op, err)
	}
	return delta, nil
}
