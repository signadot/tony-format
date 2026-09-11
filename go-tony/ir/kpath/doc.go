// Package kpath provides kinded path parsing and navigation.
//
// Kinded paths encode both navigation and structure type in the syntax:
//   - .field - Object field access
//   - [index] - Dense array index
//   - {index} - Sparse array/map index
//   - (key) - Keyed list element, in an array tagged !key(...)
//   - .* / [*] / {*} - Wildcards
//   - .. - Any depth, the node itself included
//
// A path starts at its first segment, and a first field is written without its
// dot. A field name holding path syntax is quoted: "a.b" names one field.
//
// # Usage
//
//	// Parse a kinded path
//	kp, err := kpath.Parse("users[0].name")
//
//	// Access path components
//	seg := kp.LastSegment()
//	kind := seg.EntryKind() // FieldEntry, ArrayEntry, SparseArrayEntry, KeyEntry or DescendEntry
//
//	// Navigate
//	parent := kp.Parent()                               // users[0]
//	email := kpath.ChildField(parent.String(), "email") // "users[0].email"
//
//	// Compare paths
//	cmp := kp1.Compare(kp2) // -1, 0, or 1
//
// [Parse] reports a malformed path as an error. [Split], [RSplit] and
// [SplitAll] are for paths already known to be well formed, and panic on one
// that is not.
//
// # Path Examples
//
//	"users[0].name"           // Object → array → object field
//	"data{3002}.settings"     // Object → sparse array → object field
//	"resources[*].status"     // Wildcard matching
//	"containers(web).image"   // Keyed list element → object field
//	"spec..name"              // Every field called name, at any depth under spec
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
package kpath
