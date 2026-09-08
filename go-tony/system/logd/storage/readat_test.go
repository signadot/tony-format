package storage

import (
	"fmt"
	"io"
	"slices"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// The reads, as the behavioural tests call them.
//
// Two thirds of this package's tests drive Open, NewTx and Commit and then read the result
// back. They are the specification the read interface is held to across
// wk5w1ddkh12krj1tkxn0, and what they assert is the ANSWER at a path and a commit, not the
// entry point that produced it. So they read through these rather than through a method on
// Storage, and this is the one file that moved when the entry points did: every helper here
// is Read (cursor.go) collected under testBudget.
//
// testBudget is the size of node a test is prepared to hold. Every call site of a
// materializing read states a number (read_write_interface.md); a test's number is this
// one, and a test that needs more than it is a test about size and should say so.
const testBudget = 64 << 20

// readStateAt is the whole document at commit, in the view scopeID names, root-rooted.
func readStateAt(s *Storage, kp string, commit int64, scopeID *string) (*ir.Node, error) {
	c, err := s.Read(commit, scopeID, "")
	if err != nil {
		return nil, err
	}
	doc, err := Collect(c, testBudget)
	if err != nil {
		return nil, err
	}
	return s.raiseState(scopeID, doc, ""), nil
}

// readSubtreeAt is the value at kp at commit. The second result says the store answered
// from the subtree alone, which it always does now.
func readSubtreeAt(s *Storage, kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	c, err := s.Read(commit, scopeID, kp)
	if err != nil {
		return nil, false, err
	}
	node, err := Collect(c, testBudget)
	if err != nil {
		return nil, false, err
	}
	return s.raiseState(scopeID, node, kp), true, nil
}

// readSubtreeRootedAt is readSubtreeAt with the value put back under its ancestors. A
// position cannot be rooted -- it is not a field -- and that is decided before a read is
// opened, as a caller that means to root should decide it.
func readSubtreeRootedAt(s *Storage, kp string, commit int64, scopeID *string) (*ir.Node, bool, error) {
	for _, seg := range kpath.SplitAll(kp) {
		if _, isField := kpath.SegmentFieldName(seg); !isField {
			return nil, false, fmt.Errorf("cannot root a read at %q: %q is not a field", kp, seg)
		}
	}
	c, err := s.Read(commit, scopeID, kp)
	if err != nil {
		return nil, false, err
	}
	node, err := Collect(Rooted(c, kp), testBudget)
	if err != nil {
		return nil, false, err
	}
	return s.raiseState(scopeID, node, ""), true, nil
}

// presenceAt is what a read finds at kp: absent, null, or a value.
func presenceAt(s *Storage, kp string, commit int64, scopeID *string) (Presence, error) {
	c, err := s.Read(commit, scopeID, kp)
	if err != nil {
		return Absent, err
	}
	defer c.Close()
	return c.Presence(), nil
}

// readPatchesInRange collects what Deltas walks. A test's range is small; a watch
// replay streams instead.
func readPatchesInRange(s *Storage, kp string, from, to int64, scopeID *string) ([]*CommitNotification, error) {
	cur, err := s.Deltas(from, to, scopeID, kp)
	if err != nil {
		return nil, err
	}
	defer cur.Close()
	var out []*CommitNotification
	for {
		n, err := cur.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, n)
	}
}

// segmentsAt collects the segments Read would consult for kp in [from, to].
func segmentsAt(idx *index.Index, kp string, from, to *int64, scopeID *string) []index.LogSegment {
	return slices.Collect(idx.Segments(kp, from, to, scopeID))
}

// eachPatchInRange walks Deltas, handing each to fn until fn errs.
func eachPatchInRange(s *Storage, kp string, from, to int64, scopeID *string, fn func(*CommitNotification) error) error {
	cur, err := s.Deltas(from, to, scopeID, kp)
	if err != nil {
		return err
	}
	defer cur.Close()
	for {
		n, err := cur.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(n); err != nil {
			return err
		}
	}
}
