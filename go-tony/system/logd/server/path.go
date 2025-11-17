package server

import (
	"fmt"
	"strings"
)

// validateDataPath validates a data path (not /api/transactions).
// Valid paths:
// - Must start with /
// - Must not be empty or just /
// - Must not contain .. (parent directory references)
// - Must not contain empty segments (e.g., // or /path//segment)
// - Must not contain control characters or other invalid characters
func validateDataPath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	
	if path[0] != '/' {
		return fmt.Errorf("path must start with /")
	}
	
	if path == "/" {
		return fmt.Errorf("path cannot be just /")
	}
	
	// Check for .. (parent directory references)
	if strings.Contains(path, "..") {
		return fmt.Errorf("path cannot contain ..")
	}
	
	// Check for empty segments (consecutive slashes)
	if strings.Contains(path, "//") {
		return fmt.Errorf("path cannot contain empty segments")
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
