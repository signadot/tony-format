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
	p, err := kpath.Parse(kp)
	if err != nil {
		return nil, err
	}
	return node.getKPath(p)
}

// getKPath is the internal implementation of GetKPath.
func (node *Node) getKPath(kp *kpath.KPath) (*Node, error) {
	if kp == nil {
		return node.Clone(), nil
	}
	res := node
	for kp != nil {
		if kp.FieldAll {
			return nil, fmt.Errorf("any field .* in get")
		}
		if kp.IndexAll {
			return nil, fmt.Errorf("any index [*] in get")
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
			// Sparse array handling - for now, treat as regular array index
			// This might need adjustment when sparse arrays are fully implemented
			if res.Type != ArrayType {
				return nil, fmt.Errorf("expected array for sparse index, got %s", res.Type)
			}
			index := *kp.SparseIndex
			if index < 0 || index >= len(res.Values) {
				return nil, fmt.Errorf("sparse index out of bounds %d (len %d)", index, len(res.Values))
			}
			res = res.Values[index]
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

// ListKPath traverses an ir.Node tree and collects all nodes matching a kinded path.
// Similar to ListPath() but uses kinded path syntax.
//
// Returns a slice of matching nodes.
func (node *Node) ListKPath(dst []*Node, kp string) ([]*Node, error) {
	p, err := kpath.Parse(kp)
	if err != nil {
		return nil, err
	}
	return node.listKPath(dst, p)
}

// listKPath is the internal implementation of ListKPath.
func (node *Node) listKPath(dst []*Node, kp *kpath.KPath) ([]*Node, error) {
	if kp == nil {
		return append(dst, node.Clone()), nil
	}
	var err error
	switch node.Type {
	case ObjectType:
		if kp.Index != nil || kp.IndexAll || kp.SparseIndex != nil || kp.SparseIndexAll {
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
		if kp.Field != nil || kp.FieldAll || kp.Index != nil || kp.IndexAll || kp.SparseIndex != nil || kp.SparseIndexAll {
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
