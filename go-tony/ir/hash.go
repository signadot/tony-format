package ir

import (
	"encoding/binary"
	"hash/fnv"
	"math"
)

// Hash returns a 64-bit content hash of the node: its tag, its value, its
// structure and its comments.
//
// It is the hash of [Node.DeepEqualWithComments], and of that one only. Which
// equality a hash answers for is the whole of what makes it usable as an identity
// or dedup key, and this one used to answer for neither: it left the TAG out, so
// {a: 1} and !delete {a: 1} -- which no equality here calls equal -- hashed the
// same, and anything deduplicating on it would have merged a value with its own
// deletion. It counts comments, which DeepEqual does not, so it is the document
// question rather than the value one.
//
// The hash is deterministic: it depends only on the node's structure and values,
// not on process state, so n.Hash() == n.Hash() across calls and across runs. (It uses FNV-1a rather than maphash precisely for this: a
// maphash.Hash zero value takes a fresh random seed per instance, so it would drift
// on every call — see tony-format issue f69agjyeh12ks item 16.)
//
// The hash is order-dependent for arrays and objects: reordering fields changes it.
// It panics if n is nil.
func (n *Node) Hash() uint64 {
	if n == nil {
		panic("ir: Hash called on nil node")
	}

	h := fnv.New64a()
	var b [8]byte

	// 1. Hash Type
	h.Write([]byte{byte(n.Type)})

	// 2. Hash Tag. A tag is what a node MEANS -- !key, !delete, !raw -- so two
	// nodes carrying different ones are different nodes, which DeepEqual has always
	// said and this did not.
	h.Write([]byte(n.Tag))

	// 3. Hash Value
	switch n.Type {
	case NullType:
	case CommentType:
		for _, ln := range n.Lines {
			h.Write([]byte(ln))
		}

	case BoolType:
		if n.Bool {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
	case NumberType:
		if n.Int64 != nil {
			binary.LittleEndian.PutUint64(b[:], uint64(*n.Int64))
			h.Write(b[:])
		} else if n.Float64 != nil {
			f := *n.Float64
			if f == 0 {
				// -0.0 and 0.0 compare equal in DeepEqual, which uses Go's ==,
				// but their Float64bits differ by the sign bit. Hashing them
				// apart breaks equal-implies-same-hash, so anything keyed on
				// Hash could hold both as distinct entries while every
				// comparison said they were the same value.
				f = 0
			}
			binary.LittleEndian.PutUint64(b[:], math.Float64bits(f))
			h.Write(b[:])
		} else {
			h.Write([]byte(n.Number))
		}
	case StringType:
		h.Write([]byte(n.String))
	case ArrayType:
		for _, v := range n.Values {
			// Combine child hashes order-dependently by writing each child's hash.
			binary.LittleEndian.PutUint64(b[:], v.Hash())
			h.Write(b[:])
		}
	case ObjectType:
		for i, field := range n.Fields {
			// Hash Key
			binary.LittleEndian.PutUint64(b[:], field.Hash())
			h.Write(b[:])

			// Hash Value
			binary.LittleEndian.PutUint64(b[:], n.Values[i].Hash())
			h.Write(b[:])
		}
	}
	if n.Comment != nil {
		binary.LittleEndian.PutUint64(b[:], n.Comment.Hash())
		h.Write(b[:])
	}
	return h.Sum64()
}
