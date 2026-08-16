package tx

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// An operation which calls out to the system does not belong in a stored delta,
// and mergeop has said which those are since before logd stored anything:
// mergeop.Unsafe, and a RejectUnsafe option to refuse them while applying. Nothing
// set it. The option was written for exactly this and never wired up.
//
// api.NextState now sets it, so no stored patch executes anything on any of the
// eleven paths logd applies patches on. That alone is not enough, because it
// answers too late: a store which has already accepted such a patch can only fail
// every read of it afterwards, which is the same permanent loss as a write past the
// end of an array (7cdvym1fh12ksmd5g5n0). So the write is refused too, here, while
// the client still holds the call.
//
// Why it matters is not that the client is untrusted -- every logd session is
// inside the trust boundary, and a client which can store a !pipe already has
// whatever the daemon has. It is that a stored operation which re-evaluates is not
// a VALUE. Reading the same commit twice returned two different documents, and
// logd applies stored patches in three separate places -- a full read, the stepped
// head, a watch's deltas -- so they stop agreeing with each other: the head is
// dropped for a divergence it did not cause, and a snapshot fixes one run's output
// into the base while the log keeps replaying another. History stops being
// addressable, which is the property the log is for (trqgmd1ah12kranxg5n0).

// UnsafeOpError is what a write gets when its data holds an operation which calls
// out to the system. Typed so the session can report it as the client's mistake --
// invalid_diff -- rather than as a failure of the store.
type UnsafeOpError struct {
	Path string // the write path, as the client gave it
	Op   string // the operation, without its '!'
}

func (e *UnsafeOpError) Error() string {
	return fmt.Sprintf("the patch at %q uses !%s, which calls out to the system: a stored "+
		"operation runs again on every read, replay and snapshot, so it states no value and "+
		"the log stops meaning one thing per commit", e.Path, e.Op)
}

// checkUnsafeWrite refuses a patch which holds an operation that executes. It reads
// no document and takes no lock: it is a property of the patch.
func checkUnsafeWrite(p *api.Patch) error {
	if p == nil || p.Data == nil {
		return nil
	}
	if op, found := mergeop.FindUnsafe(p.Data); found {
		return &UnsafeOpError{Path: p.Path, Op: op}
	}
	return nil
}
