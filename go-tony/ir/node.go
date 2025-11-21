package ir

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
)

type Node struct {
	Type        Type
	Parent      *Node
	ParentIndex int
	ParentField string
	Fields      []*Node
	Values      []*Node

	Tag     string
	Lines   []string
	Comment *Node

	String  string
	Bool    bool
	Number  string
	Float64 *float64
	Int64   *int64
}

func (y *Node) WithTag(tag string) *Node {
	y.Tag = tag
	return y
}

func (y *Node) Clone() *Node {
	res := &Node{}
	return y.CloneTo(res)
}

func (y *Node) CloneTo(dst *Node) *Node {
	dst.Parent = y.Parent
	dst.ParentIndex = y.ParentIndex
	dst.ParentField = y.ParentField
	dst.Type = y.Type
	dst.Tag = y.Tag
	dst.Values = make([]*Node, len(y.Values))
	dst.Fields = make([]*Node, len(y.Fields))
	for i, yv := range y.Values {
		dstI := &Node{}
		yv.CloneTo(dstI)
		dstI.Parent = dst
		dstI.ParentIndex = i
		dstI.ParentField = yv.ParentField
		dst.Values[i] = dstI
	}
	for i, yf := range y.Fields {
		dstI := &Node{}
		yf.CloneTo(dstI)
		dstI.Parent = dst
		dstI.ParentIndex = i
		dstI.ParentField = yf.String
		if yf.Type == NumberType {
			dstI.ParentField = strconv.FormatInt(*yf.Int64, 10)
		}
		dst.Fields[i] = dstI
	}

	dst.String = y.String
	dst.Number = y.Number
	if y.Float64 != nil {
		f := *y.Float64
		dst.Float64 = &f
	}
	if y.Int64 != nil {
		i := *y.Int64
		dst.Int64 = &i
	}
	dst.Bool = y.Bool
	if y.Comment != nil {
		dstComment := &Node{}
		y.Comment.CloneTo(dstComment)
		dst.Comment = dstComment
	}
	return dst
}

func (y *Node) NonCommentParent() *Node {
	p := y.Parent
	if p == nil {
		return nil
	}
	if p.Type == CommentType {
		return p.Parent
	}
	return p
}

func FromString(v string) *Node {
	return FromStringAt(&Node{}, v)
}

func FromStringAt(p *Node, v string) *Node {
	p.Type = StringType
	p.String = v
	return p
}

func FromInt(v int64) *Node {
	return &Node{
		Type:  NumberType,
		Int64: &v,
	}
}

func FromFloat(f float64) *Node {
	return &Node{
		Type:    NumberType,
		Float64: &f,
	}
}

func FromBool(v bool) *Node {
	return &Node{
		Type: BoolType,
		Bool: v,
	}
}

func ToMap(node *Node) map[string]*Node {
	if node.Type != ObjectType {
		return nil
	}
	res := make(map[string]*Node, len(node.Fields))
	for i := range node.Fields {
		field := node.Fields[i]
		if field.Type == NullType {
			continue
		}
		res[field.String] = node.Values[i]
	}
	return res
}

func FromMap(yMap map[string]*Node) *Node {
	res := &Node{}
	res.Type = ObjectType
	res.Fields = make([]*Node, len(yMap))
	res.Values = make([]*Node, len(yMap))
	keys := slices.Sorted(maps.Keys(yMap))
	for i, key := range keys {
		y := yMap[key]
		y.Parent = res
		y.ParentIndex = i
		y.ParentField = key
		yField := &Node{
			Parent:      res,
			ParentIndex: i,
			ParentField: key,
			Type:        StringType,
			String:      key,
		}
		res.Fields[i] = yField
		res.Values[i] = y
	}
	return res
}

func FromIntKeysMap(yMap map[uint32]*Node) *Node {
	return FromIntKeysMapAt(&Node{}, yMap)
}

func FromIntKeysMapAt(res *Node, yMap map[uint32]*Node) *Node {
	res.Tag = TagCompose(IntKeysTag, nil, res.Tag)
	res.Type = ObjectType
	res.Fields = make([]*Node, len(yMap))
	res.Values = make([]*Node, len(yMap))
	keys := slices.Sorted(maps.Keys(yMap))
	for i, key := range keys {
		i64Key := int64(key)
		y := yMap[key]
		y.Parent = res
		y.ParentIndex = i
		keyStr := strconv.FormatInt(int64(key), 10)
		y.ParentField = keyStr
		yField := &Node{
			Parent:      res,
			ParentIndex: i,
			ParentField: keyStr,
			Type:        NumberType,
			Int64:       &i64Key,
			String:      keyStr,
		}
		res.Fields[i] = yField
		res.Values[i] = y
	}
	return res
}

func (y *Node) ToIntKeysMap() (map[uint32]*Node, error) {
	res := make(map[uint32]*Node, len(y.Values))
	for i, field := range y.Fields {
		if field.Type != NumberType {
			return nil, fmt.Errorf("wrong type for int keys field %s (%q)", field.Type, field.String)
		}
		if field.Int64 == nil {
			return nil, fmt.Errorf("no int val for int keys field")
		}
		key := uint32(*field.Int64)
		res[key] = y.Values[i]
	}
	return res, nil
}

type KeyVal struct {
	Key *Node
	Val *Node
}

func FromKeyVals(kvs []KeyVal) *Node {
	res := &Node{}
	return FromKeyValsAt(res, kvs)
}

func FromKeyValsAt(res *Node, kvs []KeyVal) *Node {
	res.Type = ObjectType
	res.Fields = make([]*Node, len(kvs))
	res.Values = make([]*Node, len(kvs))
	for i := range kvs {
		kv := &kvs[i]
		if kv.Key == nil {
			kv.Key = &Node{Type: NullType}
		} else if kv.Key.Type == StringType {
			kv.Key.ParentField = kv.Key.String
			kv.Val.ParentField = kv.Key.ParentField
		}
		kv.Val.Parent = res
		kv.Val.ParentIndex = i
		kv.Key.Parent = res
		kv.Key.ParentIndex = i
		res.Fields[i] = kv.Key
		res.Values[i] = kv.Val
	}
	return res
}

func FromSlice(ySlice []*Node) *Node {
	res := &Node{
		Type: ArrayType,
	}
	res.Values = make([]*Node, len(ySlice))
	for i, y := range ySlice {
		res.Values[i] = y
		y.Parent = res
		y.ParentIndex = i
	}
	return res
}

func Get(y *Node, field string) *Node {
	n := len(y.Fields)
	for i := range n {
		if y.Fields[i].String == field {
			return y.Values[i]
		}
	}
	return nil
}

func Null() *Node {
	return &Node{Type: NullType}
}

func (y *Node) Visit(f func(y *Node, isPost bool) (bool, error)) error {
	dive, err := f(y, false)
	if err != nil {
		return err
	}
	if dive {
		for _, yy := range y.Values {
			if err := yy.Visit(f); err != nil {
				return err
			}
		}
	}
	if _, err := f(y, true); err != nil {
		return err
	}
	return nil
}

// used when a tag overrides a default built in tag
func (y *Node) ReType() {
	if y.Type != StringType {
		return
	}
	v := y.String
	switch v {
	case "null":
		y.Type = NullType
		return
	case "true":
		y.Type = BoolType
		y.Bool = true
		return
	case "false":
		y.Type = BoolType
		y.Bool = false
		return
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err == nil {
		y.Type = NumberType
		y.Int64 = &i
		return
	}
	f, err := strconv.ParseFloat(v, 64)
	if err == nil {
		y.Type = NumberType
		y.Float64 = &f
	}
}

func (y *Node) Root() *Node {
	res := y
	for res.Parent != nil {
		res = res.Parent
	}
	return res
}

func (node *Node) FromTonyIR(o *Node) error {
	*node = *o.Clone()
	return nil
}

func (node *Node) ToTonyIR() (*Node, error) {
	return node.Clone(), nil
}
