package ir

import "fmt"

type Type int

const (
	NullType Type = iota
	NumberType
	StringType
	BoolType
	ObjectType
	ArrayType
	CommentType
)

func (t Type) String() string {
	s, ok := map[Type]string{
		ObjectType:  "Object",
		ArrayType:   "Array",
		StringType:  "String",
		NumberType:  "Number",
		BoolType:    "Bool",
		NullType:    "Null",
		CommentType: "Comment",
	}[t]
	if ok {
		return s
	}
	return "<unknown type>"
}

func (t Type) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t *Type) UnmarshalText(d []byte) error {
	tt, ok := map[string]Type{
		"Comment": CommentType,
		"Null":    NullType,
		"Bool":    BoolType,
		"Number":  NumberType,
		"String":  StringType,
		"Array":   ArrayType,
		"Object":  ObjectType,
	}[string(d)]
	if !ok {
		return fmt.Errorf("unrecognized type %q", d)
	}
	*t = tt
	return nil

}

func Types() []Type {
	return []Type{
		NullType,
		NumberType,
		StringType,
		BoolType,
		ObjectType,
		ArrayType,
		CommentType,
	}
}

func (t Type) IsLeaf() bool {
	switch t {
	case ObjectType, ArrayType, CommentType:
		return false
	default:
		return true
	}
}
