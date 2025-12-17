// Package format provides formatting utilities for Tony documents.
//
// # Usage
//
//	// Format a Tony document
//	formatted, err := format.Format(input)
//
//	// Format with specific options
//	formatted, err := format.Format(input, format.WithIndent(2))
//
// Formatting preserves semantic content while applying consistent style.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/parse - Parse text to IR
//   - github.com/signadot/tony-format/go-tony/encode - Encode IR to text
package format
