package ir

import (
	"fmt"
	"strconv"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/token"
)

// KPath returns the kinded path string representation of this node's position in the tree.
// Similar to Path() but returns kinded path syntax (e.g., "a.b[0]" instead of "$.a.b[0]").
//
// Examples:
//   - Root node → ""
//   - Object field "a" → "a"
//   - Array element at index 0 → "[0]"
//   - Nested object "a.b" → "a.b"
//   - Mixed "a[0].b" → "a[0].b"
func (node *Node) KPath() string {
	if node.Parent == nil {
		return ""
	}
	switch node.Parent.Type {
	case ObjectType:
		f := node.ParentField
		prefix := node.Parent.KPath()
		// Quote field if it contains spaces, dots, brackets, braces, or other special characters
		var quotedField string
		if token.KPathQuoteField(f) {
			quotedField = token.Quote(f, true)
		} else {
			quotedField = f
		}
		if prefix == "" {
			return quotedField
		}
		return prefix + "." + quotedField

	case ArrayType:
		indexStr := strconv.Itoa(node.ParentIndex)
		prefix := node.Parent.KPath()
		return prefix + "[" + indexStr + "]"

	case CommentType:
		return node.Parent.KPath()

	default:
		panic("parent but not in container")
	}
}

// GetKPath navigates an ir.Node tree using a kinded path.
// Similar to GetPath() but uses kinded path syntax.
//
// Example:
//
//	rootNode.GetKPath("a.b.c") navigates to rootNode.Values["a"].Values["b"].Values["c"]
//
// Returns an error if the path doesn't exist or is invalid.
func (node *Node) GetKPath(kp string) (*Node, error) {
	return node.GetKPathWith(kp)
}

// GetKPathWith is GetKPath with navigation options.  See NavOpt: the walk sees
// through comments either way, and the option says whether the node it answers
// with keeps the one it carries.
func (node *Node) GetKPathWith(kp string, opts ...NavOpt) (*Node, error) {
	p, err := kpath.Parse(kp)
	if err != nil {
		return nil, err
	}
	res, err := node.getKPath(p)
	if err != nil {
		return nil, err
	}
	return navCfg(opts).answer(res), nil
}

// getKPath is the internal implementation of GetKPath.
func (node *Node) getKPath(kp *kpath.KPath) (*Node, error) {
	if kp == nil {
		return node.Clone(), nil
	}
	res := node
	for kp != nil {
		// A comment wraps the value it describes, and a path names the value.
		res = Uncomment(res)
		if kp.FieldAll {
			return nil, fmt.Errorf("any field .* in get")
		}
		if kp.IndexAll {
			return nil, fmt.Errorf("any index [*] in get")
		}
		if kp.Descend {
			return nil, fmt.Errorf("any depth .. in get: it may name many nodes, so ask list")
		}
		if kp.SparseIndexAll {
			return nil, fmt.Errorf("any sparse index {*} in get")
		}
		if kp.Index != nil {
			if res.Type != ArrayType {
				return nil, fmt.Errorf("expected array, got %s", res.Type)
			}
			index := *kp.Index
			if index < 0 || index >= len(res.Values) {
				return nil, fmt.Errorf("index out of bounds %d (len %d)", index, len(res.Values))
			}
			res = res.Values[index]
			kp = kp.Next
			continue
		}
		if kp.SparseIndex != nil {
			// A sparse array is an OBJECT whose field keys are numbers -- there is no
			// SparseArrayType -- so {7} names the value under the key 7, not the
			// seventh value. This used to require an Array and index it positionally,
			// which is two different wrong answers: the type never matched, and had
			// it matched, {7} of {3: a, 7: b} would have been read off the end.
			//
			// It is the same node shape logd writes {n} paths FROM: index.indexPatchRec
			// and extractTopLevelKPaths both take the key from the field's own Int64.
			if res.Type != ObjectType {
				return nil, fmt.Errorf("expected a sparse array (an object with number keys) "+
					"for {%d}, got %s", *kp.SparseIndex, res.Type)
			}
			val := sparseValue(res, *kp.SparseIndex)
			if val == nil {
				return nil, nil // no such key, which is an absence and not a fault
			}
			res = val
			kp = kp.Next
			continue
		}
		if kp.Key != nil {
			if res.Type != ArrayType {
				return nil, fmt.Errorf("expected array for key, got %s", res.Type)
			}
			elems := keyedElems(res, *kp.Key)
			if len(elems) == 0 {
				return nil, nil // no element carries that key
			}
			res = elems[0]
			kp = kp.Next
			continue
		}
		if kp.Field != nil {
			if res.Type != ObjectType {
				return nil, fmt.Errorf("expected object, got %s", res.Type)
			}
			field := *kp.Field
			found := false
			for i, yf := range res.Fields {
				if yf.String != field {
					continue
				}
				res = res.Values[i]
				kp = kp.Next
				found = true
				break
			}
			if found {
				continue
			}
			return nil, nil // Path doesn't exist
		}
		if kp.Next != nil {
			return nil, fmt.Errorf("unexpected next segment without index or field")
		}
		return res.Clone(), nil
	}
	return res.Clone(), nil
}

// KeyField reports the field a keyed list keys its elements by: the argument of
// the !key(...) tag the array carries.  keyed is false for a node without the
// tag, whose elements have no keys, so a (key) path segment reaches nothing in
// it.  The field is "" for a bare !key, whose elements are their own keys.
//
// The field is itself a kpath, so a list may be keyed by something nested:
// !key(metadata.name).
func (node *Node) KeyField() (field string, keyed bool) {
	head, args := TagGet(node.Tag, KeyTag)
	if head == "" {
		return "", false
	}
	if len(args) == 0 {
		return "", true
	}
	return args[0], true
}

// keyedElems returns the elements of a keyed list which carry key.  A list may
// hold a key twice -- nothing in the format forbids it -- and a walk which
// silently took the first would hide the second, so all of them come back.
func keyedElems(arr *Node, key string) []*Node {
	field, keyed := arr.KeyField()
	if !keyed {
		return nil
	}
	var res []*Node
	for _, elem := range arr.Values {
		k, ok := ElemKey(elem, field)
		if !ok || k != key {
			continue
		}
		res = append(res, elem)
	}
	return res
}

// ElemKey is the key of one element of a keyed list as a (key) path segment
// spells it: the text of the scalar at the key field, which is itself a kpath
// into the element, and the element itself when the field is "".  A key which is
// not a scalar names nothing, there being no way for a path segment to spell it.
//
// Whatever names an element for a walk has to name it for whoever writes the
// path down, so a caller which records keyed paths -- an index, a log -- reads
// them from here rather than from its own idea of what a key looks like.
func ElemKey(elem *Node, field string) (string, bool) {
	k := elem
	if field != "" {
		var err error
		k, err = elem.GetKPath(field)
		if err != nil || k == nil {
			return "", false
		}
	}
	switch k.Type {
	case StringType:
		return k.String, true
	case BoolType:
		return strconv.FormatBool(k.Bool), true
	case NumberType:
		return k.numberText(), true
	}
	return "", false
}

// numberText is the text of a number node, whether it was read as an integer, a
// float, or kept as the digits it came in as.
func (node *Node) numberText() string {
	switch {
	case node.Number != "":
		return node.Number
	case node.Int64 != nil:
		return strconv.FormatInt(*node.Int64, 10)
	case node.Float64 != nil:
		return strconv.FormatFloat(*node.Float64, 'g', -1, 64)
	}
	return ""
}

// ListKPath traverses an ir.Node tree and collects all nodes matching a kinded path.
// Similar to ListPath() but uses kinded path syntax.
//
// Returns a slice of matching nodes.
func (node *Node) ListKPath(dst []*Node, kp string) ([]*Node, error) {
	return node.ListKPathWith(dst, kp)
}

// ListKPathWith is ListKPath with navigation options.  See NavOpt.
//
// Without options the answers are VALUES, as GetKPath's is: a path names the
// value whatever was said about it. The two disagreed -- Get uncommented its
// answer and List handed back the comment wrapper -- so the same path through
// the same document gave a different node depending on which of them was asked
// (8rr738ffh12kr3t8g5n0 made that reachable, by keeping the comment that had
// been dropped).
func (node *Node) ListKPathWith(dst []*Node, kp string, opts ...NavOpt) ([]*Node, error) {
	p, err := kpath.Parse(kp)
	if err != nil {
		return nil, err
	}
	start := len(dst)
	res, err := node.listKPath(dst, p)
	if err != nil {
		return nil, err
	}
	// Only what this call found: dst may carry a caller's earlier results, which
	// are not ours to answer for.
	cfg := navCfg(opts)
	for i := start; i < len(res); i++ {
		res[i] = cfg.answer(res[i])
	}
	return res, nil
}

// listKPath is the internal implementation of ListKPath.
func (node *Node) listKPath(dst []*Node, kp *kpath.KPath) ([]*Node, error) {
	if kp == nil {
		return append(dst, node.Clone()), nil
	}
	// A comment wraps the value it describes, and a path names the value. The
	// switch below has no case for a comment, so a walk which met one answered
	// with NOTHING and no error -- the one shape of this bug that is silent.
	// getKPath and listPath were taught this; this one was missed
	// (3cdjz00jh12krns4g1n0).
	node = Uncomment(node)
	var err error
	// `..` offers this node and every node beneath it to what follows, which is
	// what makes a..x find an x directly under a as well as one further down. The
	// walk is here rather than in the switch below because it is not a step into a
	// container: an array, an object and a leaf all descend the same way.
	if kp.Descend {
		if kp.Next == nil {
			// A trailing `..` names everything under here, and here itself.
			return node.appendAll(dst), nil
		}
		if err := node.visitAll(func(n *Node) error {
			dst, err = n.listKPath(dst, kp.Next)
			return err
		}); err != nil {
			return nil, err
		}
		return dst, nil
	}
	switch node.Type {
	case ObjectType:
		// A sparse array is an object with number keys, so {7} and {*} are answered
		// here rather than falling to the kind-mismatch below with everything else.
		if kp.SparseIndex != nil {
			if val := sparseValue(node, *kp.SparseIndex); val != nil {
				dst, err = val.listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		if kp.SparseIndexAll {
			for i := range node.Fields {
				if node.Fields[i].Type != NumberType {
					continue
				}
				dst, err = node.Values[i].listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		// A dense index or a key names nothing in an object, which is an answer and
		// not a fault: a query walks nodes of every kind -- `..x` visits leaves,
		// arrays and objects alike -- so a segment which does not fit the node it
		// meets has to be a non-match rather than an error.
		if kp.Index != nil || kp.IndexAll || kp.Key != nil {
			return dst, nil
		}
		if kp.Field == nil && !kp.FieldAll && kp.Next == nil {
			return append(dst, node.Clone()), nil
		}
		if kp.FieldAll {
			// Iterate all object fields
			for i := range node.Fields {
				dst, err = node.Values[i].listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		if kp.Field != nil {
			field := *kp.Field
			for i := range node.Fields {
				if node.Fields[i].String != field {
					continue
				}
				dst, err = node.Values[i].listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
		}
		return dst, nil

	case ArrayType:
		if kp.Field != nil || kp.FieldAll {
			return dst, nil
		}
		if kp.Key != nil {
			// a list may hold the same key twice; naming a key names each
			// element carrying it, as a wildcard names each element it reaches
			for _, elem := range keyedElems(node, *kp.Key) {
				dst, err = elem.listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		if kp.Index == nil && !kp.IndexAll && kp.SparseIndex == nil && !kp.SparseIndexAll && kp.Next == nil {
			return append(dst, node.Clone()), nil
		}
		if kp.Index != nil {
			idx := *kp.Index
			if 0 <= idx && idx < len(node.Values) {
				dst, err = node.Values[idx].listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		if kp.IndexAll {
			// Iterate all array elements
			for _, yv := range node.Values {
				dst, err = yv.listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		if kp.SparseIndexAll {
			// Iterate all sparse array elements (for now, treat as regular array)
			for _, yv := range node.Values {
				dst, err = yv.listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		if kp.SparseIndex != nil {
			idx := *kp.SparseIndex
			if 0 <= idx && idx < len(node.Values) {
				dst, err = node.Values[idx].listKPath(dst, kp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		return dst, nil

	case StringType, NumberType, NullType, BoolType:
		if kp.Field != nil || kp.FieldAll || kp.Index != nil || kp.IndexAll || kp.SparseIndex != nil || kp.SparseIndexAll || kp.Key != nil {
			return dst, nil
		}
		if kp.Next == nil {
			dst = append(dst, node.Clone())
			return dst, nil
		}
		return dst, nil
	default:
		return dst, nil
	}
}

// visitAll offers node and every node beneath it, in document order. It is the
// walk `..` is defined by, and it is a walk and not a match: what to do with each
// node is the caller's.
func (node *Node) visitAll(fn func(*Node) error) error {
	if node == nil {
		return nil
	}
	node = Uncomment(node)
	if node == nil {
		return nil
	}
	if err := fn(node); err != nil {
		return err
	}
	for _, v := range node.Values {
		if err := v.visitAll(fn); err != nil {
			return err
		}
	}
	return nil
}

// appendAll answers with node and everything beneath it, which is what a path
// ending in `..` names.
func (node *Node) appendAll(dst []*Node) []*Node {
	_ = node.visitAll(func(n *Node) error {
		dst = append(dst, n.Clone())
		return nil
	})
	return dst
}

// sparseValue answers the value a sparse array holds under key, or nil when it
// holds none.
//
// A sparse array is an object whose field keys are numbers: {3: a, 7: b} holds b
// under 7, at position 1. The key is the field's own value, which is where every
// producer of a {n} path takes it from, so a lookup takes it from the same place.
func sparseValue(node *Node, key int) *Node {
	if node == nil || node.Type != ObjectType {
		return nil
	}
	for i := range node.Fields {
		f := node.Fields[i]
		if f.Type != NumberType || f.Int64 == nil {
			continue
		}
		if int(*f.Int64) == key {
			return node.Values[i]
		}
	}
	return nil
}
