package schema

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A parameterized definition has to CHECK its parameter.  base.tony wrote the
// element clause of array(t) as `!all.t null`, which substituted nothing --
// there is no way to spell a match as a tag component -- and an unrecognized
// tag in a pattern is ignored, so .[array(int)] accepted a list of anything and
// reported it as a pass.  Issue bsbxmm0th12kshdwg9n0.
func TestElementTypeIsChecked(t *testing.T) {
	// flowPerson is written in flow style, so its body wears a presentation tag
	// that !all composes over: the element check has to survive that.
	const defs = `
define:
  person:
    name: .[string]
  flowPerson: {name: .[string]}
`
	tests := []struct {
		name   string
		accept string
		doc    string
		want   bool
	}{{
		name:   "array of numbers, all numbers",
		accept: ".[array(number)]",
		doc:    "- 1\n- 2\n",
		want:   true,
	}, {
		name:   "array of numbers, one string",
		accept: ".[array(number)]",
		doc:    "- 1\n- hello\n",
		want:   false,
	}, {
		name:   "array of numbers, one object",
		accept: ".[array(number)]",
		doc:    "- 1\n- {a: 1}\n",
		want:   false,
	}, {
		name:   "an empty array is an array of anything",
		accept: ".[array(number)]",
		doc:    "[]\n",
		want:   true,
	}, {
		name:   "the container is still checked",
		accept: ".[array(number)]",
		doc:    "a: 1\n",
		want:   false,
	}, {
		name:   "array of a defined type",
		accept: ".[array(person)]",
		doc:    "- {name: a}\n- {name: b}\n",
		want:   true,
	}, {
		name:   "array of a defined type, one wrong",
		accept: ".[array(person)]",
		doc:    "- {name: a}\n- {name: 3}\n",
		want:   false,
	}, {
		name:   "array of a defined type, one not an object at all",
		accept: ".[array(person)]",
		doc:    "- 3\n",
		want:   false,
	}, {
		name:   "array of a flow-written type",
		accept: ".[array(flowPerson)]",
		doc:    "- {name: a}\n",
		want:   true,
	}, {
		name:   "array of a flow-written type, one wrong",
		accept: ".[array(flowPerson)]",
		doc:    "- {name: a}\n- {name: 3}\n",
		want:   false,
	}, {
		name:   "nested arrays",
		accept: ".[array(array(number))]",
		doc:    "- [1]\n- [2, 3]\n",
		want:   true,
	}, {
		name:   "nested arrays, inner element wrong",
		accept: ".[array(array(number))]",
		doc:    "- [1]\n- [x]\n",
		want:   false,
		// sparsearray(t) carries the same element clause, and is not tested
		// here: nothing can satisfy it yet.  sparsearray's own key clause,
		// !all.field.type 0, asks each key to be a number, and !field hands
		// over the key's NAME, which is always a string -- so .[sparsearray]
		// accepts nothing, and whether that is even reported depends on map
		// order, since a definition index keyed by base name holds either
		// sparsearray or sparsearray(t) and not both.  Both reported with
		// this issue.
	}, {
		// object(t) named a parameter and used it nowhere at all
		name:   "object values",
		accept: ".[object(number)]",
		doc:    "a: 1\nb: 2\n",
		want:   true,
	}, {
		name:   "object values, one wrong",
		accept: ".[object(number)]",
		doc:    "a: 1\nb: x\n",
		want:   false,
	}, {
		name:   "nullable takes the null",
		accept: ".[nullable(person)]",
		doc:    "null\n",
		want:   true,
	}, {
		name:   "nullable takes the type",
		accept: ".[nullable(person)]",
		doc:    "name: a\n",
		want:   true,
	}, {
		// !t null made nullable(t) accept anything, the same way
		name:   "nullable takes nothing else",
		accept: ".[nullable(person)]",
		doc:    "name: 3\n",
		want:   false,
	}, {
		name:   "an array of nullables",
		accept: ".[array(nullable(number))]",
		doc:    "- 1\n- null\n",
		want:   true,
	}, {
		name:   "an array of nullables, one wrong",
		accept: ".[array(nullable(number))]",
		doc:    "- 1\n- x\n",
		want:   false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := baseSchema(t, defs+"accept: "+test.accept+"\n")
			doc, err := parse.Parse([]byte(test.doc))
			if err != nil {
				t.Fatalf("parse document: %s", err)
			}
			err = s.Validate(doc)
			switch {
			case test.want && err != nil:
				t.Errorf("%s rejected %q: %s", test.accept, test.doc, err)
			case !test.want && err == nil:
				t.Errorf("%s accepted %q", test.accept, test.doc)
			}
		})
	}
}

// A parameter named in a tag is a mistake to report, not a check to drop: it is
// bound to a match, which no tag component spells.
func TestParameterInTagPositionIsAnError(t *testing.T) {
	s := baseSchema(t, `
define:
  person:
    name: .[string]
  elems(t): !all.t null
accept: .[elems(person)]
`)
	doc, err := parse.Parse([]byte("- {name: a}\n"))
	if err != nil {
		t.Fatalf("parse document: %s", err)
	}
	err = s.Validate(doc)
	if err == nil {
		t.Fatal("a definition which cannot check its parameter validated a document")
	}
	if !strings.Contains(err.Error(), `parameter "t"`) {
		t.Errorf("error does not name the parameter:\n%s", err)
	}
}

// The tag a parameter wore composes over the tag its argument has, rather than
// being dropped: !all t with t bound to `!and [...]` is `!all.and [...]`, and
// dropping the !all matched the container against the element type.
func TestInstantiateComposesTags(t *testing.T) {
	body, err := parse.Parse([]byte("!all t\n"))
	if err != nil {
		t.Fatalf("parse body: %s", err)
	}
	arg, err := parse.Parse([]byte("!and\n- !irtype 1\n"))
	if err != nil {
		t.Fatalf("parse arg: %s", err)
	}
	got, err := InstantiateDef(body, []string{"t"}, []*ir.Node{arg})
	if err != nil {
		t.Fatalf("instantiate: %s", err)
	}
	if got.Tag != "!all.and" {
		t.Errorf("tag is %q, want %q", got.Tag, "!all.and")
	}
}

// baseSchema reads a schema the way `o schema check` does, with base.tony's
// definitions filled in.
func baseSchema(t *testing.T, src string) *Schema {
	t.Helper()
	node, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse schema: %s", err)
	}
	if err := MergeBaseDefinitions(node); err != nil {
		t.Fatalf("base definitions: %s", err)
	}
	s, err := ParseSchema(node)
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	return s
}
