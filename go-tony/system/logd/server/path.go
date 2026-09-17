package server

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// pathRole is what a path in a request is FOR, which is what decides whether it may
// name a set. A query segment -- a wildcard (.* [*] {*} (*)) naming the nodes at one
// level, or `..` naming them at any depth -- names a set. A read answers a set, one node
// at a time (MatchResult), so it takes either. A write and a watch do not: a patch is
// rooted at one place, and a watch is one stream of one path's state.
//
// The three roles are the three call sites, so the question is asked where the answer
// differs rather than by every caller separately.
type pathRole int

const (
	rolePatternRead pathRole = iota // a match: a wildcard names the set it answers
	roleWrite                       // a patch, and the precondition it carries
	roleWatch                       // a watch or an unwatch
)

// validateDataPath validates a kpath for the role it arrived in.
// Empty path ("") is valid and refers to the root.
//
// A query segment is refused everywhere a path must name a place -- what a patch is
// rooted at, what a watch names, what the index is keyed by -- and taken by a read,
// which answers the set it names. A `..` is refused with its own words, since it is a
// question at any depth rather than one level, and a path holding one turned into
// something obscure further in when it was not refused at the door (`ir node
// unspecified`, from a merge which cannot tell what kind of container the segment
// stepped into). A wildcard used to be refused nowhere: a write took one and stored a
// field literally named `*`, which no read could then reach (pvre1n2fh12ksmptn5n0), and
// a watch took one, was confirmed, and was then ended by its own first event
// (t55dmsthh12kssetn5n0).
func validateDataPath(path string, role pathRole) error {
	kp, err := kpath.Parse(path)
	if err != nil {
		return err
	}
	if role == rolePatternRead {
		return nil
	}
	for x := kp; x != nil; x = x.Next {
		if x.Descend {
			return fmt.Errorf("%q: `..` names nodes at any depth, which is a question and not "+
				"a place: a path here has to name one", path)
		}
		if x.Wild() {
			return fmt.Errorf("%q: segment %q names a set of values, and a path here has to "+
				"name one", path, x.SegmentString())
		}
	}
	return nil
}

// kpathHasWild says the path holds a wildcard segment, which makes it name a set: a
// read answers one, and nothing else takes one. A path that does not parse holds
// nothing -- it is refused by the validator that ran before this.
func kpathHasWild(path string) bool {
	kp, err := kpath.Parse(path)
	if err != nil {
		return false
	}
	for x := kp; x != nil; x = x.Next {
		if x.Wild() {
			return true
		}
	}
	return false
}

// kpathHasDescend says the path holds a `..`, which is what a depth bounds.
func kpathHasDescend(path string) bool {
	kp, err := kpath.Parse(path)
	if err != nil {
		return false
	}
	for x := kp; x != nil; x = x.Next {
		if x.Descend {
			return true
		}
	}
	return false
}
