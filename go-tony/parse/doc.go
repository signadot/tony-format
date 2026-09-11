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
//	// Parse YAML, keeping comments
//	node, err = parse.Parse(data, parse.ParseYAML(), parse.ParseComments(true))
//
//	// Parse a stream of documents separated by "---"
//	nodes, err := parse.ParseMulti(data)
//
// The input is read as Tony unless an option selects another format:
// [ParseYAML], [ParseJSON] or [ParseFormat]. Valid JSON is valid Tony, so Tony
// mode reads JSON as well. [ParseNodeFromSource] reads the next bracketed value
// or scalar from a [token.TokenSource].
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/encode - Encode IR to text
//   - github.com/signadot/tony-format/go-tony/token - Tokenization
package parse
