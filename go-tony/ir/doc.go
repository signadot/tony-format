// Package ir provides the intermediate representation for Tony format documents.
//
// Tony documents are represented as trees of ir.Node values. Nodes can be:
// atomic (null, bool, number, string), composite (object, array), or comments.
// Any node can carry a tag, in its Tag field.
//
// # Creating Nodes
//
//	obj := ir.FromMap(map[string]*ir.Node{
//	    "name": ir.FromString("alice"),
//	    "age":  ir.FromInt(30),
//	})
//	arr := ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromInt(2)})
//
// [FromMap] orders an object's fields by key; [FromKeyVals] keeps the order it
// is given.
//
// # Parent Links
//
// Parent, ParentIndex and ParentField say where a node sits: the container
// holding it, its position there, and, in an object, its key. The constructors
// set them on the nodes they are given. The links are coherent: walking down
// from a node to a child and back up by Parent arrives at the node again. A
// node with no Parent is a document root, and [Node.Root] walks up to it.
//
// # Navigation
//
//	child, err := node.GetKPath("users[0].name")
//	path := child.KPath() // "users[0].name"
//
// Paths are kinded paths (see package kpath). Navigation sees through comments
// to the value they describe. [Node.GetKPath] answers a copy of the node a path
// names, and (nil, nil) when the document holds no field, sparse index or key
// the path names; [Node.ListKPath] collects every node a path names, wildcards
// and `..` included.
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
// Comments are what was said about a value, not part of it: [Node.DeepEqual],
// [Compare] and [Truth] see through them, while [Node.DeepEqualWithComments]
// and [Node.Hash] count them.
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
