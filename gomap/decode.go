// Package codec provides Go to Tony encoding and Tony to Go decoding.
package gomap

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/signadot/tony-format/tony/format"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/parse"
)

type fromOpts struct {
	comments bool
	format   format.Format
}

func (do *fromOpts) parseOpts() []parse.ParseOption {
	res := []parse.ParseOption{
		parse.ParseComments(do.comments),
		parse.ParseFormat(do.format),
	}
	return res
}

type FromOption func(*fromOpts)

func LoadComments(v bool) FromOption        { return func(o *fromOpts) { o.comments = v } }
func LoadFormat(f format.Format) FromOption { return func(o *fromOpts) { o.format = f } }

type IRFromer interface {
	FromIR(*ir.Node, ...FromOption) error
}

type WithTager interface {
	WithTag(string) error
}

type WithComments interface {
	WithHeadComment(hd []string) error
	WithLineComments(ln []string) error
}

type WithTrailingComments interface {
	WithTrailingComments([]string) error
}

func Load(d []byte, p any, opts ...FromOption) error {
	do := &fromOpts{}
	for _, f := range opts {
		f(do)
	}
	if x, ok := p.(FromIR); ok {
		node, err := parse.Parse(d, do.parseOpts()...)
		if err != nil {
			return err
		}
		return x.FromIR(node, opts...)
	}
	node, err := parse.Parse(d, do.parseOpts()...)
	if err != nil {
		return err
	}
	b := bytes.NewBuffer(nil)
	jEnc := jsontext.NewEncoder(b)
	encErr := nodeToJEnc(node, jEnc)
	jDec := jsontext.NewDecoder(b)
	if err := json.UnmarshalDecode(jDec, p); err != nil {
		return err
	}
	if encErr != nil {
		return encErr
	}
	// we have the result of json unmarshaling via ir, now check for
	return nil
}

func nodeToJEnc(node *ir.Node, je *jsontext.Encoder) error {
	switch node.Type {
	case ir.CommentType:
		return nodeToJEnc(node.Values[0], je)
	case ir.ObjectType:
		if err := je.WriteToken(jsontext.BeginObject); err != nil {
			return err
		}
		for i, field := range node.Fields {
			val := node.Values[i]
			if err := je.WriteToken(jsontext.String(field.String)); err != nil {
				return err
			}
			if err := nodeToJEnc(val, je); err != nil {
				return err
			}
		}
		return je.WriteToken(jsontext.EndObject)
	case ir.ArrayType:
		if err := je.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		for _, val := range node.Values {
			if err := nodeToJEnc(val, je); err != nil {
				return err
			}
		}
		return je.WriteToken(jsontext.EndArray)

	case ir.StringType:
		return je.WriteToken(jsontext.String(node.String))
	case ir.NumberType:
		if node.Int64 != nil {
			return je.WriteToken(jsontext.Int(*node.Int64))
		}
		if node.Float64 != nil {
			return je.WriteToken(jsontext.Float(*node.Float64))
		}
		panic("number")
	case ir.BoolType:
		return je.WriteToken(jsontext.Bool(node.Bool))
	case ir.NullType:
		return je.WriteToken(jsontext.Null)
	default:
		panic("ir type")
	}
}
