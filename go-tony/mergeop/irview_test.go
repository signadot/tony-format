package mergeop

import (
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// The view is what !ir matches against: the fields of ir.Node under the names it
// serializes them with, and only the ones the node has.
func TestIRView(t *testing.T) {
	tests := []struct {
		name string
		doc  *ir.Node
		want map[string]string // field -> what it should hold, encoded loosely
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
		name: "a tag is a field of the view",
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
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view := IRView(test.doc)
			if view.Type != ir.ObjectType {
				t.Fatalf("view is a %s", view.Type)
			}
			got := map[string]string{}
			for i, f := range view.Fields {
				got[f.String] = scalarText(view.Values[i])
			}
			for name, want := range test.want {
				if got[name] != want {
					t.Errorf("view.%s = %q, want %q", name, got[name], want)
				}
			}
			for name := range got {
				if _, ok := test.want[name]; !ok {
					t.Errorf("view has %s = %q, which the node does not have", name, got[name])
				}
			}
		})
	}
}

// The view keeps the document's own children, so a pattern which reaches them is
// at a place in the document rather than in a copy of it.
func TestIRViewKeepsTheDocumentsChildren(t *testing.T) {
	doc := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)})
	view := IRView(doc)
	var values *ir.Node
	for i, f := range view.Fields {
		if f.String == "values" {
			values = view.Values[i]
		}
	}
	if values == nil {
		t.Fatal("an object's view has no values field")
	}
	if len(values.Values) != 1 || values.Values[0] != doc.Values[0] {
		t.Errorf("values holds a copy, not the document's own child")
	}
	// and the view itself is nowhere in the document, so an explanation of a
	// failure against it is reported at the node !ir was applied to
	if view.Parent != nil {
		t.Errorf("the view has a parent, so it claims to be a place in the document")
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
