package mergeop

import (
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// !ir asks about the fields of ir.Node under the names it serializes them with,
// and only about the ones the node HAS: an unset field is absent, not null.
//
// This used to be a question about a whole object built to stand for the node.
// It is now one field at a time, so the same facts are asserted one field at a
// time -- including the absences, which are what a pattern naming the field
// turns into a mismatch.
func TestIRFieldsOfANode(t *testing.T) {
	tests := []struct {
		name string
		doc  *ir.Node
		want map[string]string // field -> what it holds, encoded loosely; absent if not listed
	}{{
		name: "an integer has an int field and no float field",
		doc:  ir.FromInt(3),
		want: map[string]string{"type": "Number", "int": "3"},
	}, {
		name: "a float has a float field and no int field",
		doc:  ir.FromFloat(3.5),
		want: map[string]string{"type": "Number", "float": "3.5"},
	}, {
		name: "a string",
		doc:  ir.FromString("x"),
		want: map[string]string{"type": "String", "string": "x"},
	}, {
		name: "a tag is a field of the representation",
		doc:  ir.FromString("v").WithTag("!k"),
		want: map[string]string{"type": "String", "string": "v", "tag": "!k"},
	}, {
		// Bool is not a pointer, so false is indistinguishable from unset here:
		// the reason base.tony's bool stays !irtype true.
		name: "false has no bool field",
		doc:  ir.FromBool(false),
		want: map[string]string{"type": "Bool"},
	}, {
		name: "true does",
		doc:  ir.FromBool(true),
		want: map[string]string{"type": "Bool", "bool": "true"},
	}, {
		name: "null has nothing but its type",
		doc:  ir.Null(),
		want: map[string]string{"type": "Null"},
	}, {
		// a container has both, empty ones included
		name: "an empty object still has fields and values",
		doc:  ir.FromKeyVals(nil),
		want: map[string]string{"type": "Object", "fields": "Array", "values": "Array"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, field := range irFields {
				got, has := irFieldOf(test.doc, field)
				want, wanted := test.want[field]
				if has != wanted {
					t.Errorf("%s: present=%v, want %v", field, has, wanted)
					continue
				}
				if !has {
					continue
				}
				if txt := scalarText(got); txt != want {
					t.Errorf("%s = %q, want %q", field, txt, want)
				}
			}
		})
	}
}

// The list a pattern reaching fields or values is matched against is a VALUE: a
// coherent little document standing for part of the node.
//
// It used to hold the document's own children while being parentless itself, so
// it could be walked down and not back up -- one node the library's invariant did
// not hold for, and the reason Explain has to find its root by node identity
// (p4tzbzx7h12kr6tkhxn0).
func TestIRChildListIsItsOwnCoherentTree(t *testing.T) {
	doc := ir.FromKeyVals([]ir.KeyVal{{Key: ir.FromString("a"), Val: ir.FromInt(1)}})

	for _, field := range []string{"fields", "values"} {
		t.Run(field, func(t *testing.T) {
			list, has := irFieldOf(doc, field)
			if !has {
				t.Fatalf("an object has no %s", field)
			}
			if list.Parent != nil {
				t.Errorf("the list has a parent, so it claims to be a place in the document")
			}
			if len(list.Values) != 1 {
				t.Fatalf("%s holds %d children, want 1", field, len(list.Values))
			}
			kid := list.Values[0]
			if kid.Parent != list {
				t.Errorf("the list's child points at %v, not at the list: it can be walked "+
					"down and not back up", kid.Parent)
			}
			// and the document is untouched: its own children still belong to it
			own := doc.Fields[0]
			if field == "values" {
				own = doc.Values[0]
			}
			if kid == own {
				t.Errorf("the list holds the document's own child, which the list re-parents")
			}
			if own.Parent != doc {
				t.Errorf("the document's own child was re-parented out of it")
			}
		})
	}
}

func scalarText(n *ir.Node) string {
	switch n.Type {
	case ir.StringType:
		return n.String
	case ir.BoolType:
		return strconv.FormatBool(n.Bool)
	case ir.NumberType:
		if n.Int64 != nil {
			return strconv.FormatInt(*n.Int64, 10)
		}
		if n.Float64 != nil {
			return strconv.FormatFloat(*n.Float64, 'g', -1, 64)
		}
		return n.Number
	}
	return n.Type.String()
}
