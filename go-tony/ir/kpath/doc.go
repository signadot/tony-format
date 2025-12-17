// Package kpath provides kinded path parsing and navigation.
//
// Kinded paths encode both navigation and structure type in the syntax:
//   - .field - Object field access
//   - [index] - Dense array index
//   - {index} - Sparse array/map index
//   - .* / [*] / {*} - Wildcards
//
// # Usage
//
//	// Parse a kinded path
//	kp, err := kpath.Parse("users[0].name")
//
//	// Access path components
//	seg := kp.LastSegment()
//	kind := seg.EntryKind() // FieldEntry, ArrayEntry, or SparseArrayEntry
//
//	// Navigate
//	parent := kp.Parent()
//	child := kp.Append(kpath.Field("email"))
//
//	// Compare paths
//	cmp := kp1.Compare(kp2) // -1, 0, or 1
//
// # Path Examples
//
//	"users[0].name"           // Object → array → object field
//	"data{3002}.settings"     // Object → sparse array → object field
//	"resources[*].status"     // Wildcard matching
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
package kpath
