// Package ir contains the Tony format implementation.
package ir

import (
	"encoding/json"
	"fmt"
)

type irBase struct {
	Type   Type    `json:"type"`
	Fields []*Node `json:"fields,omitempty"`
	Values []*Node `json:"values,omitempty"`

	Tag     string   `json:"tag,omitempty"`
	Lines   []string `json:"lines,omitempty"`
	Comment *Node    `json:"comment,omitempty"`
	Number  string   `json:"number,omitempty"`
	Float64 *float64 `json:"float,omitempty"`
	Int64   *int64   `json:"int,omitempty"`
}

func (y *Node) MarshalJSON() ([]byte, error) {

	base := &irBase{
		Type:    y.Type,
		Fields:  y.Fields,
		Values:  y.Values,
		Tag:     y.Tag,
		Lines:   y.Lines,
		Comment: y.Comment,
		Number:  y.Number,
		Float64: y.Float64,
		Int64:   y.Int64,
	}
	switch y.Type {
	case StringType:
		type C struct {
			irBase
			String string `json:"string"`
		}
		return json.Marshal(C{irBase: *base, String: y.String})
	case BoolType:
		type C struct {
			irBase
			Bool bool `json:"bool"`
		}
		return json.Marshal(C{irBase: *base, Bool: y.Bool})
	default:
		return json.Marshal(base)
	}
}

func (y *Node) UnmarshalJSON(d []byte) error {
	type C struct {
		irBase
		String string `json:"string"`
		Bool   bool   `json:"bool"`
	}
	tmp := &C{irBase: irBase{}}
	if err := json.Unmarshal(d, tmp); err != nil {
		return err
	}
	y.Type = tmp.Type
	y.Values = tmp.Values
	y.Fields = tmp.Fields
	y.Comment = tmp.Comment
	y.Bool = tmp.Bool
	y.String = tmp.String
	y.Lines = tmp.Lines
	y.Number = tmp.Number
	y.Int64 = tmp.Int64
	y.Float64 = tmp.Float64
	y.Tag = tmp.Tag

	switch y.Type {
	case ObjectType:
		// An object holds one value per field, and this is where that is checked:
		// what follows reads y.Fields[i] for every value, so a document with more
		// values than fields used to panic here rather than be reported -- `o load`
		// on a malformed file died with a stack trace. The other way round was
		// worse for being quiet: it was accepted, and produced a node whose fields
		// and values were misaligned for everything downstream to trip over.
		if len(y.Fields) != len(y.Values) {
			return fmt.Errorf("malformed object with %d fields and %d values",
				len(y.Fields), len(y.Values))
		}
		var fType *Type
		for i, f := range y.Fields {
			f.Parent = y
			f.ParentIndex = i
			f.ParentField = f.String
			// A null key is a string key. The normalization is the point of this
			// branch, and the line which used to follow the switch -- fType =
			// &f.Type -- put the raw type back, so the one shape it exists to
			// accept was rejected with the very error it exists to prevent.
			kind := f.Type
			if kind == NullType {
				kind = StringType
			}
			if fType == nil {
				switch kind {
				case StringType, NumberType:
				default:
					return fmt.Errorf("invalid field type %s", f.Type)
				}
				fType = &kind
				continue
			}
			if *fType != kind {
				return fmt.Errorf("mixed field types %s and %s", *fType, f.Type)
			}
		}
		for i, v := range y.Values {
			v.Parent = y
			v.ParentIndex = i
			v.ParentField = y.Fields[i].String
			if v.Type == CommentType && len(v.Values) != 1 {
				return fmt.Errorf("malformed head comment with %d values", len(v.Values))
			}
		}
	case ArrayType:
		for i, v := range y.Values {
			v.Parent = y
			v.ParentIndex = i
			if v.Type == CommentType && len(v.Values) != 1 {
				return fmt.Errorf("malformed head comment with %d values", len(v.Values))
			}
		}
	case CommentType:
		switch len(y.Values) {
		case 0:
		case 1:
			wrapped := y.Values[0]
			wrapped.Parent = y
			wrapped.ParentIndex = 0
		default:
			return fmt.Errorf("malformed comment with %d values", len(y.Values))
		}
	}
	return nil
}
