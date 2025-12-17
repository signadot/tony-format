// Package ir provides the intermediate representation for Tony format documents.
//
// Tony documents are represented as trees of ir.Node values. Nodes can be:
// atomic (null, bool, number, string), composite (object, array), or metadata
// (comments, tags).
//
// # Creating Nodes
//
//	obj := ir.FromMap(map[string]*ir.Node{
//	    "name": ir.FromString("alice"),
//	    "age":  ir.FromInt(30),
//	})
//	arr := ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2)})
//
// # Navigation
//
//	child, err := node.GetKPath("users[0].name")
//	path := node.KPath() // "users[0].name"
//
// # Node Types
//
// The Type field indicates node type: NullType, BoolType, NumberType,
// StringType, ArrayType, ObjectType, CommentType.
//
// # Objects
//
// For ObjectType, Fields[i] is the key for Values[i]. Field keys are either:
//   - String nodes (normal object keys)
//   - Int nodes fitting uint32 (sparse array keys)
//   - Null nodes (merge keys, may repeat)
//
// Objects have all int keys or all non-int keys (no mixing).
//
// # Numbers
//
// Number values use Int64 (64-bit signed), Float64 (64-bit IEEE), or Number
// (string fallback).
//
// # Comments
//
// CommentType nodes represent head comments (Values[0] is the commented node)
// or line comments (in the Comment field of another node).
//
// # Thread Safety
//
// Nodes are not thread-safe. Synchronize access or clone for concurrent use.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/parse - Parse text to IR
//   - github.com/signadot/tony-format/go-tony/encode - Encode IR to text
//   - github.com/signadot/tony-format/go-tony/schema - Schema validation
package ir
