// Package format names the document formats this module reads and writes:
// [TonyFormat], [YAMLFormat] and [JSONFormat].
//
// # Usage
//
//	// Read a format's name, as a flag or a config field gives it
//	f, err := format.ParseFormat("yaml")
//
//	// Parse and encode in that format
//	node, err := parse.Parse(data, parse.ParseFormat(f))
//	err = encode.Encode(node, w, encode.EncodeFormat(f))
//
// A [Format] marshals to text as its long name ("tony", "yaml", "json") and
// unmarshals from any name [ParseFormat] reads.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/parse - Parse text to IR
//   - github.com/signadot/tony-format/go-tony/encode - Encode IR to text
package format
