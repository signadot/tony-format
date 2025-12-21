package server

import "fmt"

// validateDataPath validates a kpath.
// Empty path ("") is valid and refers to the root.
// Paths must not contain control characters.
func validateDataPath(path string) error {
	// Empty path is valid (root)
	if path == "" {
		return nil
	}

	// Check for invalid characters (control characters, etc.)
	for _, r := range path {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return fmt.Errorf("path contains invalid character")
		}
		if r == 0 {
			return fmt.Errorf("path contains null character")
		}
	}

	return nil
}
