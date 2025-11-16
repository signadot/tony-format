package ir

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

func (y *Node) Path() string {
	if y.Parent == nil {
		return "$"
	}
	switch y.Parent.Type {
	case ObjectType:
		f := y.ParentField
		prefix := y.Parent.Path() + "."
		if f != "" && strings.IndexAny(f, "'.*$[]") == -1 {
			return prefix + f
		}
		return prefix + "'" + strings.Replace(f, "'", "\\'", -1) + "'"

	case ArrayType:
		return y.Parent.Path() + "[" + strconv.Itoa(y.ParentIndex) + "]"
	case CommentType:
		return y.Parent.Path()
	default:
		panic("parent but not in container")
	}
}

type Path struct {
	IndexAll bool
	Index    *int
	Field    *string
	Subtree  bool
	Next     *Path
}

func (p *Path) String() string {
	buf := bytes.NewBuffer([]byte{'$'})
	x := p
	for x != nil {
		if x.Subtree {
			buf.WriteString("..")
			x = x.Next
			continue
		}
		if x.IndexAll {
			buf.WriteString("[*]")
			x = x.Next
			continue
		}
		if x.Field != nil {
			buf.WriteString("." + *x.Field)
			x = x.Next
			continue
		}
		if x.Index != nil {
			fmt.Fprintf(buf, "[%d]", *x.Index)
			x = x.Next
			continue
		}
		x = x.Next
	}
	return buf.String()

}

func ParsePath(p string) (*Path, error) {
	if len(p) == 0 || p[0] != '$' {
		return nil, fmt.Errorf("path %q should start with '$'", p)
	}
	root := &Path{}
	if len(p) == 1 {
		return root, nil
	}
	err := parseFrag(p[1:], root)
	if err != nil {
		return nil, err
	}
	return root, nil
}

func parseFrag(frag string, parent *Path) error {
	if len(frag) == 0 {
		return nil
	}
	switch frag[0] {
	case '.':
		if len(frag) > 1 && frag[1] == '.' {
			parent.Subtree = true
			next := &Path{}
			err := parseFrag(frag[2:], next)
			if err != nil {
				return err
			}
			parent.Next = next
			return nil
		}
		field, rest, err := parseField(frag[1:])
		if err != nil {
			return err
		}
		parent.Field = &field
		if len(rest) == 0 {
			return nil
		}
		next := &Path{}
		err = parseFrag(rest, next)
		if err != nil {
			return err
		}
		parent.Next = next
		return nil
	case '[':
		i := strings.IndexByte(frag[1:], ']')
		if i == -1 {
			return fmt.Errorf("expected '[' <index> ']'")
		}
		index, all, err := parseIndex(frag[1 : i+1])
		if err != nil {
			return err
		}
		parent.IndexAll = all
		if !all {
			parent.Index = &index
		}
		if len(frag) == i+2 {
			return nil
		}
		next := &Path{}
		err = parseFrag(frag[i+2:], next)
		if err != nil {
			return err
		}
		parent.Next = next
		return nil
	default:
		return fmt.Errorf("expected '.' or '['")
	}
}

func parseIndex(is string) (index int, all bool, err error) {
	if len(is) == 1 && is[0] == '*' {
		return 0, true, nil
	}
	u64, err := strconv.ParseUint(is, 10, 64)
	if err != nil {
		return 0, false, err
	}
	return int(u64), false, nil
}

func parseField(frag string) (field, rest string, err error) {
	if len(frag) == 0 {
		return "", "", fmt.Errorf("expected field at end of string")
	}
	if frag[0] != '\'' {
		i := strings.IndexAny(frag, ".[")
		if i == -1 {
			return frag, "", nil
		}
		return frag[:i], frag[i:], nil
	}
	escaped := false
	res := make([]byte, 0, len(frag))
	for i := 1; i < len(frag); i++ {
		c := frag[i]
		switch c {
		case '\\':
			escaped = true
		case '\'':
			if !escaped {
				return string(res), frag[i+1:], nil
			}
			fallthrough
		default:
			escaped = false
			res = append(res, c)
		}
	}
	return "", "", fmt.Errorf("end of string scanning for \"'\"")
}

func (y *Node) GetPath(yPath string) (*Node, error) {
	yp, err := ParsePath(yPath)
	if err != nil {
		return nil, err
	}
	res := y
	for yp != nil {
		if yp.IndexAll {
			return nil, fmt.Errorf("any index in get")
		}
		if yp.Subtree {
			return nil, fmt.Errorf("recurse .. in get")
		}
		if yp.Index != nil {
			if res.Type != ArrayType {
				return nil, fmt.Errorf("expected array, got %s", y.Type)
			}
			index := *yp.Index
			if index < 0 || index >= len(res.Values) {
				return nil, fmt.Errorf("index out of bounds %d (len %d)", index, len(y.Values))
			}
			res = res.Values[index]
			yp = yp.Next
			continue
		}
		if yp.Field != nil {
			if res.Type != ObjectType {
				return nil, fmt.Errorf("expected object got %s", res.Type)
			}
			field := *yp.Field
			found := false
			for i, yf := range res.Fields {
				if yf.String != field {
					continue
				}
				res = res.Values[i]
				yp = yp.Next
				found = true
				break
			}
			if found {
				continue
			}
			return nil, nil
		}
		if yp.Next != nil {
			return nil, fmt.Errorf("unexpected next w/out index or field")
		}
		return res.Clone(), nil
	}
	return res.Clone(), nil
}

func pathString(f string) string {
	if strings.IndexAny(f, "'.*$[]") == -1 {
		return f
	}
	return "'" + strings.Replace(f, "'", "\\'", -1) + "'"
}

func (y *Node) ListPath(dst []*Node, yPath string) ([]*Node, error) {
	yp, err := ParsePath(yPath)
	if err != nil {
		return nil, err
	}
	return y.listPath(dst, yp)
}

func (y *Node) listPath(dst []*Node, yp *Path) ([]*Node, error) {
	if yp == nil {
		return append(dst, y.Clone()), nil
	}
	var err error
	if yp.Subtree {
		if err := y.Visit(func(node *Node, isPost bool) (bool, error) {
			if isPost {
				return false, nil
			}
			if y.Type.IsLeaf() {
				return false, nil
			}
			dst, err = node.listPath(dst, yp.Next)
			if err != nil {
				return false, err
			}
			return true, nil
		}); err != nil {
			return nil, err
		}
		return dst, nil
	}
	switch y.Type {
	case ObjectType:
		if yp.IndexAll || yp.Index != nil {
			return dst, nil
		}

		if yp.Field == nil && yp.Next == nil {
			return append(dst, y.Clone()), nil
		}

		field := *yp.Field
		for i := range y.Fields {
			if y.Fields[i].String != field {
				continue
			}
			dst, err = y.Values[i].listPath(dst, yp.Next)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil

	case ArrayType:
		if yp.Field != nil {
			return dst, nil
		}
		if yp.Index == nil && !yp.IndexAll && yp.Next == nil {
			return append(dst, y.Clone()), nil
		}
		if yp.Index != nil {
			idx := *yp.Index
			if 0 <= idx && idx < len(y.Values) {
				dst, err = y.Values[idx].listPath(dst, yp.Next)
				if err != nil {
					return nil, err
				}
			}
			return dst, nil
		}
		if !yp.IndexAll {
			return dst, nil
		}
		for _, yv := range y.Values {
			dst, err = yv.listPath(dst, yp.Next)
			if err != nil {
				return nil, err
			}
		}
		return dst, nil

	case StringType, NumberType, NullType, BoolType:
		if yp.Field != nil || yp.Index != nil || yp.IndexAll {
			return dst, nil
		}
		if yp.Next == nil {
			dst = append(dst, y.Clone())
			return dst, nil
		}
		return dst, nil
	default:
		panic("type")
	}
}
