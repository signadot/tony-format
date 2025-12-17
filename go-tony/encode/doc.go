// Package encode encodes IR nodes to Tony format text.
//
// # Usage
//
//	// Encode to Tony format
//	node := ir.FromMap(map[string]*ir.Node{
//	    "name": ir.FromString("alice"),
//	    "age":  ir.FromInt(30),
//	})
//	output, err := encode.Encode(node)
//
//	// Encode with options
//	output, err := encode.Encode(node, encode.WithIndent(2))
//
//	// Encode to JSON
//	output, err := encode.EncodeJSON(node)
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/parse - Parse text to IR
package encode
