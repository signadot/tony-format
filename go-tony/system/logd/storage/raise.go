package storage

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/ident"
)

// Raising: the stored form of a keyed array is an object of names, and the form a client
// reads and writes is an array (element_identity.md, tx.LowerKeyed). This is the way back,
// applied at the boundary -- to a state a read answers with and to a delta a watch is
// handed -- and nowhere inside the store, which never needs the array.
//
// It is a function on a node that shares what it does not change: an object that is not
// a keyed array comes back as the same pointer, and a container is rebuilt only where
// something under it was.
//
// The schema is the authority on which paths are keyed; a stored container carries no
// tag saying so. A tag would say the same thing twice, and it would cost every element
// write its spine at the array -- passesThrough reads any label that is not
// presentation as an operator (index/log_segment.go) -- which is the one read cost
// identity exists to remove.

// SchemaFor answers the schema in force for the view scopeID names, or nil for a store
// without one. It is what a caller outside the store needs to spell an element's name.
func (s *Storage) SchemaFor(scopeID *string) *api.Schema {
	return s.schemaForScope(scopeID)
}

// RaiseState puts array-ness back on the keyed arrays in a STATE at document path kp in
// the view scopeID names -- what a caller at the boundary does to a node it collected from
// Read before handing it to a client. A state is op-free: the arrays come back untagged,
// as a client's own fold of the deltas leaves them. The store's own readers never call it;
// the write path speaks the stored form.
func (s *Storage) RaiseState(scopeID *string, n *ir.Node, kp string) *ir.Node {
	return s.raise(scopeID, n, kp, false)
}

func (s *Storage) raiseState(scopeID *string, n *ir.Node, kp string) *ir.Node {
	return s.RaiseState(scopeID, n, kp)
}

// raiseDelta puts array-ness back on the keyed arrays in a DELTA rooted at the document.
// A delta's arrays carry !key(f) where the identity is one field, so the client's merge
// identifies the elements the way the store does; folding such a delta consumes the
// operation and leaves the untagged array a state read answers with.
func (s *Storage) raiseDelta(scopeID *string, n *ir.Node) *ir.Node {
	return s.raise(scopeID, n, "", true)
}

func (s *Storage) raise(scopeID *string, n *ir.Node, kp string, delta bool) *ir.Node {
	schema := s.schemaForScope(scopeID)
	if schema == nil || n == nil {
		return n
	}
	at, atElement, err := schemaPathOfRead(schema, kp)
	if err != nil {
		return n
	}
	return raiseKeyed(schema, n, at, atElement, delta)
}

// schemaPathOfRead is where a read's answer sits in the schema's terms: names under keyed
// arrays elided, and whether the answer is an element.
func schemaPathOfRead(schema *api.Schema, path string) (at string, atElement bool, err error) {
	if path == "" {
		return "", false, nil
	}
	kp, err := kpath.Parse(path)
	if err != nil {
		return "", false, err
	}
	for x := kp; x != nil; x = x.Next {
		atElement = false
		switch {
		case x.Field != nil:
			if schema.Keyed(at) && ident.IsName(*x.Field) {
				atElement = true
				continue
			}
			at = kpath.ChildField(at, *x.Field)
		default:
			at += x.SegmentString()
		}
	}
	return at, atElement, nil
}

// raiseKeyed answers n with every keyed array under it in array form. at is n's schema
// path; atElement says n is an element of the keyed array at `at`; delta says n is a
// delta rather than a state, whose arrays are tagged.
func raiseKeyed(schema *api.Schema, n *ir.Node, at string, atElement bool, delta bool) *ir.Node {
	if n == nil {
		return nil
	}
	if n.Type == ir.CommentType && len(n.Values) == 1 {
		inner := raiseKeyed(schema, n.Values[0], at, atElement, delta)
		if inner == n.Values[0] {
			return n
		}
		out := n.Clone()
		out.Values[0] = inner
		inner.Parent, inner.ParentIndex = out, 0
		return out
	}
	if ir.TagHas(n.Tag, libdiff.RawTag) {
		return n
	}
	op := opOf(n)
	identity := schema.Identity(at)
	if len(identity) > 0 && !atElement && n.Type == ir.ObjectType {
		if op == "replace" {
			return rebuildFields(n, func(field string, v *ir.Node) *ir.Node {
				return raiseKeyed(schema, v, at, false, delta)
			})
		}
		return raiseArray(schema, n, at, identity, delta)
	}
	switch n.Type {
	case ir.ObjectType:
		if op == "replace" {
			return rebuildFields(n, func(field string, v *ir.Node) *ir.Node {
				return raiseKeyed(schema, v, at, atElement, delta)
			})
		}
		return rebuildFields(n, func(field string, v *ir.Node) *ir.Node {
			return raiseKeyed(schema, v, kpath.ChildField(at, field), false, delta)
		})
	case ir.ArrayType:
		changed := false
		vals := make([]*ir.Node, len(n.Values))
		for i, v := range n.Values {
			vals[i] = raiseKeyed(schema, v, fmt.Sprintf("%s[%d]", at, i), false, delta)
			changed = changed || vals[i] != v
		}
		if !changed {
			return n
		}
		out := n.Clone()
		for i, v := range vals {
			out.Values[i] = v
			v.Parent, v.ParentIndex = out, i
		}
		return out
	}
	return n
}

// raiseArray turns the object form at a keyed path into the array a client reads: the
// elements in name order. In a DELTA the array is tagged !key(f) where the identity is one
// field, so a client's own merge identifies the elements the way the store does; a field
// that is a !delete becomes an element that is a !delete carrying the identity, which is
// how that merge names what to remove, and every other element carries its identity
// because the name is stamped into it at the write. A composite identity has no !key
// spelling -- !key takes one field -- so such a delta comes back untagged.
//
// A STATE is op-free, so its arrays are untagged: that is what folding the deltas leaves,
// and a read and a fold have to agree.
func raiseArray(schema *api.Schema, n *ir.Node, at string, identity []string, delta bool) *ir.Node {
	elems := make([]*ir.Node, 0, len(n.Values))
	for i, f := range n.Fields {
		if i >= len(n.Values) {
			break
		}
		name, isName, err := ident.Parse(f.String)
		if !isName || err != nil {
			// Not the object form after all -- leave the container as it is.
			return n
		}
		v := raiseKeyed(schema, n.Values[i], at, true, delta)
		if e := ir.Uncomment(v); e != nil && opOf(e) == "delete" && e.Type != ir.ObjectType {
			carrier := ir.FromMap(map[string]*ir.Node{})
			for _, b := range name.Bindings() {
				carrier.Fields = append(carrier.Fields, ir.FromString(b.Field))
				carrier.Values = append(carrier.Values, b.Value)
			}
			carrier.Tag = e.Tag
			v = carrier
		}
		elems = append(elems, v)
	}
	out := ir.FromSlice(elems)
	out.Tag = n.Tag
	if delta && len(identity) == 1 {
		out.Tag = ir.TagCompose(ir.KeyTag, []string{identity[0]}, n.Tag)
	}
	return out
}

// rebuildFields maps an object's values and answers the same object when nothing changed.
func rebuildFields(n *ir.Node, f func(field string, v *ir.Node) *ir.Node) *ir.Node {
	changed := false
	vals := make([]*ir.Node, len(n.Values))
	for i, v := range n.Values {
		field := ""
		if i < len(n.Fields) {
			field = n.Fields[i].String
		}
		vals[i] = f(field, v)
		changed = changed || vals[i] != v
	}
	if !changed {
		return n
	}
	out := n.Clone()
	for i, v := range vals {
		out.Values[i] = v
		v.Parent, v.ParentIndex = out, i
	}
	return out
}

// opOf is the merge operation a node carries, or "" for none.
func opOf(n *ir.Node) string {
	if n == nil || n.Tag == "" {
		return ""
	}
	_, op, _, _, err := mergeop.SplitChild(n)
	if err != nil {
		return ""
	}
	return op
}
