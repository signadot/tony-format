package storage

import "github.com/signadot/tony-format/go-tony/ir"

// The reads, as the behavioural tests call them.
//
// Two thirds of this package's tests drive Open, NewTx and Commit and then read the result
// back. They are the specification the read interface is held to across
// wk5w1ddkh12krj1tkxn0, and what they assert is the ANSWER at a path and a commit, not the
// entry point that produced it. So they read through these four rather than through a
// method on Storage, and when the entry points change, this is the one file that moves.
//
// testBudget is the size of node a test is prepared to hold. Every call site of a
// materializing read states a number (read_write_interface.md); a test's number is this
// one, and a test that needs more than it is a test about size and should say so.
const testBudget = 64 << 20

// readStateAt is the whole document at commit, in the view scopeID names, root-rooted.
func readStateAt(s *Storage, kp string, commit int64, scopeID *string) (*ir.Node, error) {
	return s.ReadStateAt(kp, commit, scopeID)
}

// readSubtreeAt is the value at kp at commit. The second result says whether the store
// answered from the subtree alone.
func readSubtreeAt(s *Storage, kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	return s.ReadSubtreeAt(kp, commit, scopeID)
}

// readSubtreeRootedAt is readSubtreeAt with the value put back under its ancestors.
func readSubtreeRootedAt(s *Storage, kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	return s.ReadSubtreeRootedAt(kp, commit, scopeID)
}

// absentSpineAt is the index's proof that kp was never written, when it has one.
func absentSpineAt(s *Storage, kp string, scopeID *string) (*ir.Node, bool) {
	return s.AbsentSpineAt(kp, scopeID)
}
