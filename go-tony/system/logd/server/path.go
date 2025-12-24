package server

import (
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// validateDataPath validates a kpath.
// Empty path ("") is valid and refers to the root.
// Paths must not contain control characters.
func validateDataPath(path string) error {
	_, err := kpath.Parse(path)
	return err
}
