package server

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// validateDataPath validates a kpath.
// Empty path ("") is valid and refers to the root.
// Paths must not contain control characters.
//
// A `..` segment is refused here. It is a QUERY segment -- it names the nodes at
// any depth rather than a step to one -- and every path this validates is a path
// the store must be able to keep: what a patch is rooted at, what a match reads,
// what a watch names and what the index is keyed by. A descent has no answer as
// any of those, so it is refused where it arrives rather than turned into
// something obscure further in (`ir node unspecified`, from a merge which cannot
// tell what kind of container the segment stepped into).
func validateDataPath(path string) error {
	kp, err := kpath.Parse(path)
	if err != nil {
		return err
	}
	for x := kp; x != nil; x = x.Next {
		if x.Descend {
			return fmt.Errorf("%q: `..` names nodes at any depth, which is a question and not "+
				"a place: a path here has to name one", path)
		}
	}
	return nil
}
