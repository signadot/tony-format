// Package gomap provides encoding and decoding between IR nodes and Go values.
//
// # Usage
//
//	// Decode IR to Go struct
//	type User struct {
//	    Name string
//	    Age  int
//	}
//	var user User
//	err := gomap.Decode(node, &user)
//
//	// Encode Go value to IR
//	node, err := gomap.Encode(user)
//
//	// With options
//	node, err := gomap.Encode(user, gomap.EncodeWire(true))
//
// The package handles conversion between Tony's IR representation and
// native Go types, including structs, maps, slices, and primitives.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/gomap/codegen - Code generation
package gomap
