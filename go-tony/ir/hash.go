package ir

import (
	"encoding/binary"
	"hash/maphash"
	"math"
)

// Hash returns a 64-bit hash of the node.
// Hash includes comments
// It panics if n is nil.
func (n *Node) Hash() uint64 {
	if n == nil {
		panic("ir: Hash called on nil node")
	}

	var h maphash.Hash
	// You might want to use a shared Seed for deterministic hashing across runs if needed,
	// otherwise maphash generates a random seed per process start.

	// 1. Hash Type
	h.WriteByte(byte(n.Type))

	// 2. Hash Value
	switch n.Type {
	case NullType:
	case CommentType:
		for _, ln := range n.Lines {
			h.WriteString(ln)
		}

	case BoolType:
		if n.Bool {
			h.WriteByte(1)
		} else {
			h.WriteByte(0)
		}
	case NumberType:
		if n.Int64 != nil {
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], uint64(*n.Int64))
			h.Write(b[:])
		} else if n.Float64 != nil {
			var b [8]byte
			binary.LittleEndian.PutUint64(b[:], math.Float64bits(*n.Float64))
			h.Write(b[:])
		} else {
			h.WriteString(n.Number)
		}
	case StringType:
		h.WriteString(n.String)
	case ArrayType:
		var b [8]byte
		for _, v := range n.Values {
			// Combine child hashes.
			// Writing the child hash into the hasher is a simple way to combine them order-dependently.
			binary.LittleEndian.PutUint64(b[:], v.Hash())
			h.Write(b[:])
		}
	case ObjectType:
		var b [8]byte
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
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], n.Comment.Hash())
	}
	return h.Sum64()
}
