package server

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// pathRole is what a path in a request is FOR, which is what decides whether it may
// name a set. The two query segments differ here:
//
//   - `..` names the nodes at any depth. Nothing takes it: it is a question no
//     operation answers, and the store cannot keep it either.
//   - a wildcard (.* [*] {*} (*)) names the nodes at one level. A read answers a set,
//     one node at a time (MatchResult), so it takes one. A write and a watch do not:
//     a patch is rooted at one place, and a watch is one stream of one path's state.
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
// A `..` segment is refused in every role. It is a QUERY segment -- it names the nodes
// at any depth rather than a step to one -- and a path the store must be able to keep
// is what a patch is rooted at, what a watch names and what the index is keyed by. A
// descent has no answer as any of those, so it is refused where it arrives rather than
// turned into something obscure further in (`ir node unspecified`, from a merge which
// cannot tell what kind of container the segment stepped into). A read refuses it too,
// until an any-depth read is designed (th7sdhvyh12ksjtfn9n0).
//
// A wildcard is refused everywhere a path must name a place. It used to be refused
// nowhere: a write took one and stored a field literally named `*`, which no read
// could then reach (pvre1n2fh12ksmptn5n0), and a watch took one, was confirmed, and
// was then ended by its own first event (t55dmsthh12kssetn5n0).
func validateDataPath(path string, role pathRole) error {
	kp, err := kpath.Parse(path)
	if err != nil {
		return err
	}
	for x := kp; x != nil; x = x.Next {
		if x.Descend {
			return fmt.Errorf("%q: `..` names nodes at any depth, which is a question and not "+
				"a place: a path here has to name one", path)
		}
		if role != rolePatternRead && x.Wild() {
			return fmt.Errorf("%q: segment %q names a set of values, and a path here has to "+
				"name one", path, x.SegmentString())
		}
	}
	return nil
}
