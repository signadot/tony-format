package api

import "fmt"

// DoesNotApplyError is what a write gets when its patch cannot be applied to the
// state it would be applied to.
//
// A store which records a delta it cannot apply has not recorded a write, it has
// recorded a fault, and the fault is permanent: every read replays the log, so it
// meets that delta again every time. A later patch cannot repair it, because the
// read dies on the way past. That is one write costing the whole store -- reads of
// documents it never touched included -- and it is what made a patch one past the
// end of an array fatal (7cdvym1fh12ksmd5g5n0).
//
// Which patches can fail this way is not a property of the operation. A field
// write cannot: it states what results. An operation which asserts something about
// the base can, and its assertion can be false -- an index the array does not
// have, a !replace whose from: is not what is there, a !strdiff of something which
// is not a string. So the question is not which operations may be stored, it is
// whether THIS delta applies to THIS state, and it is asked of every write.
type DoesNotApplyError struct {
	Commit int64 // the commit the patch was to become
	Err    error // what the applier said
}

func (e *DoesNotApplyError) Error() string {
	return fmt.Sprintf("the patch does not apply to the current state, so storing it would "+
		"make every read of the store fail: %v", e.Err)
}

func (e *DoesNotApplyError) Unwrap() error { return e.Err }
