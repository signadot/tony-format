// Package encode encodes IR nodes to Tony format text.
//
// # Usage
//
//	// Encode to Tony format
//	node := ir.FromMap(map[string]*ir.Node{
//	    "name": ir.FromString("alice"),
//	    "age":  ir.FromInt(30),
//	})
//	err := encode.Encode(node, os.Stdout)
//
//	// Encode with options
//	err = encode.Encode(node, w, encode.EncodeBrackets(true), encode.EncodeComments(true))
//
//	// Encode to JSON
//	err = encode.Encode(node, w, encode.EncodeFormat(format.JSONFormat))
//
//	// Encode to a string, panicking on failure
//	s := encode.MustString(node)
//
// The output is Tony unless [EncodeFormat] selects YAML or JSON.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/parse - Parse text to IR
package encode
