package eval

import (
	"encoding/json"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func MarshalJSON(node *ir.Node) ([]byte, error) {
	return json.Marshal(ToJSONAny(node))
}

func FromJSONAny(v any) (*ir.Node, error) {
	d, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return parse.Parse(d, parse.NoBrackets())
}

func ToJSONAny(node *ir.Node) any {
	// TODO tags
	switch node.Type {
	case ir.ObjectType:
		n := len(node.Fields)
		res := make(map[string]any, n)
		for i := range n {
			field := node.Fields[i]
			if field.Type == ir.NullType {
				continue
			}
			res[field.String] = ToJSONAny(node.Values[i])
		}
		return res
	case ir.ArrayType:
		res := make([]any, len(node.Values))
		for i, elt := range node.Values {
			res[i] = ToJSONAny(elt)
		}
		return res
	case ir.StringType:
		return node.String
	case ir.NumberType:
		if node.Int64 != nil {
			return int(*node.Int64)
		}
		if node.Float64 != nil {
			return float64(*node.Float64)
		}
		return node.Number
	case ir.BoolType:
		return node.Bool
	case ir.NullType:
		return nil
	case ir.CommentType:
		return ToJSONAny(node.Values[0])
	default:
		panic("impossible production")
	}
}
