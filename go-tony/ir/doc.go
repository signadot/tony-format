// Package ir provides the intermediate representation (IR) for Tony format documents.
//
// # Overview
//
// The IR package defines the core data structures for representing Tony format
// documents as a tree of nodes. All Tony documents (whether parsed from text,
// created programmatically, or generated from schemas) are represented as
// ir.Node trees.
//
// The IR itself is a simple recursive structure that is readily representable
// in JSON, YAML, and Tony format. This makes the IR useful for manipulating
// Tony documents in contexts which lack parsing and encoding support. The IR
// contains no position information from input documents, making it purely semantic.
//
// # Node Structure
//
// A Node represents a single value in a Tony document. Nodes can be:
//
//   - Atomic types: null, boolean, number, string
//   - Composite types: object (key-value pairs), array (ordered list)
//   - Metadata: tags, comments, line information
//
// Each node maintains parent-child relationships, allowing navigation through
// the tree structure.
//
// The IR works as a recursive tagged union structure, where values are placed
// in fields depending on the node type.
//
// # Node Types
//
// The Type field indicates the node's type:
//
//   - NullType: null value
//   - BoolType: boolean (true/false)
//   - NumberType: numeric value (int64 or float64)
//   - StringType: string value
//   - ArrayType: ordered list of nodes
//   - ObjectType: key-value pairs (fields and values)
//   - CommentType: comment node
//
// # Creating Nodes
//
// Use constructor functions to create nodes:
//
//	node := ir.FromString("hello")
//	num := ir.FromInt(42)
//	flag := ir.FromBool(true)
//	obj := ir.FromMap(map[string]*ir.Node{
//	    "key": ir.FromString("value"),
//	})
//	arr := ir.FromSlice([]*ir.Node{
//	    ir.FromInt(1),
//	    ir.FromInt(2),
//	})
//
// # IR Structure Constraints
//
// The IR has specific constraints that must be maintained:
//
// ## Objects
//
// For ObjectType nodes, Fields[i] is the key for the value at Values[i], so
// there will always be the same number of fields as values.
//
// Fields are always either:
//   - String typed (and not multiline) - normal object keys
//   - Int typed (fitting in uint32) - for int-keyed maps (sparse arrays)
//   - Null typed - represents a merge key and may occur multiple times
//
// Other keys (non-null) should appear only once. Objects must either have all
// keys int typed, or all keys not int typed (mixed int/string keys are not allowed).
//
// ## Strings
//
// String canonical values are stored under the String field. If the string was
// a multiline folding Tony string, then the Lines field may contain the folding
// decomposition. Producers should not populate Lines where String is not equal
// to the concatenation of Lines. Consumers should check if they are equal and
// if not, remove the Lines decomposition and consider String canonical.
//
// ## Numbers
//
// Number values are placed under:
//   - Int64: if it is an integer (64-bit signed)
//   - Float64: if it is a floating point number (64-bit IEEE float)
//   - Number: as a string fallback if neither Int64 nor Float64 can represent it
//
// ## Comments
//
// CommentType nodes define comment association. Comment content is placed in
// the Lines field.
//
// A comment node either:
//   - Contains 1 element in Values, a non-comment node to which it is associated
//     as a head comment
//   - Contains 0 elements and resides in the Comment field of a non-comment node,
//     representing its line comment plus possibly trailing comment material
//
// A comment node may not represent both a head comment and a line comment. In the
// second case, normally it represents a single line comment (e.g., `null # comment`)
// with 1 entry in Lines. All such comments must contain all whitespace between
// the end of the value and the `#` to preserve vertical alignment.
//
// All comments not preceding any value nor occurring on the same line of any value
// are collected and appended to the Lines of the comment node residing in the
// Comment field of the root non-comment node. If that node has no line comment,
// a dummy line comment is present with value "".
//
// # Navigating Nodes
//
// Nodes maintain parent-child relationships:
//
//   - Parent: parent node (nil for root)
//   - ParentIndex: index in parent's array/object
//   - ParentField: field name if parent is object
//   - Fields: field names (for ObjectType)
//   - Values: child values (for ObjectType and ArrayType)
//
// Use Path() to get a JSONPath-style path string:
//
//	path := node.Path() // e.g., "$.foo.bar[0]"
//
// Use KPath() to get a kinded path string:
//
//	kpath := node.KPath() // e.g., "foo.bar[0]"
//
// # Path Operations
//
// The package provides two path systems:
//
//   - Path: JSONPath-style paths (e.g., "$.foo.bar[0]")
//   - KPath: Kinded paths (e.g., "foo.bar[0]") that encode node kinds in syntax
//
// Use GetPath() or GetKPath() to navigate to a node:
//
//	child, err := node.GetKPath("foo.bar[0]")
//	if err != nil {
//	    // path doesn't exist
//	}
//
// # Tags
//
// Nodes can have tags (YAML-style metadata) stored in the Tag field:
//
//	node.Tag = "!or"
//	node.Tag = "!schema(person)"
//
// Use tag manipulation functions:
//
//	ir.TagArgs("!schema(person)") // parse tag name and arguments
//	ir.TagCompose("!all", "has-path", "foo") // compose tags
//
// # Comparison and Hashing
//
// Nodes can be compared for equality:
//
//	equal := ir.Compare(a, b) == 0
//
// Nodes can be hashed (useful for caching, deduplication):
//
//	hash := ir.Hash(node)
//
// # JSON Interoperability
//
// The IR itself is representable in JSON, YAML, and Tony format, making it
// self-describing. Nodes can be converted to/from JSON:
//
//	jsonBytes, err := ir.ToJSON(node)
//	node, err := ir.FromJSON(jsonBytes)
//
// This allows the IR to be serialized and manipulated in contexts without
// Tony format support.
//
// # Thread Safety
//
// Node structures are not thread-safe. If you need to access nodes from
// multiple goroutines, you must synchronize access yourself or clone nodes
// for each goroutine.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/parse - Parses text into IR nodes
//   - github.com/signadot/tony-format/go-tony/encode - Encodes IR nodes to text
//   - github.com/signadot/tony-format/go-tony/schema - Schema system using IR
//   - github.com/signadot/tony-format/go-tony/mergeop - Operations on IR nodes
package ir
