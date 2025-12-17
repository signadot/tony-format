// Package parse parses Tony format text into IR nodes.
//
// # Usage
//
//	// Parse Tony text
//	node, err := parse.Parse([]byte(`{name: "alice", age: 30}`))
//	if err != nil {
//	    return err
//	}
//
//	// Parse from string
//	node, err := parse.ParseString(`[1, 2, 3]`)
//
//	// Parse with options
//	node, err := parse.Parse(data, parse.WithFilename("config.tony"))
//
// The parser handles Tony, YAML, and JSON formats automatically.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/encode - Encode IR to text
//   - github.com/signadot/tony-format/go-tony/token - Tokenization
package parse
