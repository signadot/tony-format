package schema

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// int and float said nothing they could say: `!and [.[number], {int: !not null}]`
// asks for a node which is both a Number and an object, so .[int] matched
// nothing at all -- `age: .[int]` rejected `age: 3`, and gomap emits .[int] for
// every Go int.  Both halves were wrong: an object pattern is about the
// document, and `!not null` matches nothing, since a bare null matches
// everything.  !ir asks the node instead.  Issue bsbxmm0th12kshdwg9n0.
func TestIntAndFloatMeanWhatTheySay(t *testing.T) {
	tests := []struct {
		accept string
		doc    string
		want   bool
	}{
		{".[int]", "3", true},
		{".[int]", "-3", true},
		{".[int]", "0", true},
		{".[int]", "3.5", false},
		{".[int]", "3e2", false}, // an exponent parses to a float
		{".[int]", `"3"`, false},
		{".[int]", "null", false},
		{".[int]", "true", false},
		// the node, not a document shaped like its representation
		{".[int]", "{int: 3}", false},
		{".[int]", "{type: Number, int: 3}", false},
		{".[float]", "3.5", true},
		{".[float]", "3e2", true},
		{".[float]", "3", false},
		{".[float]", `"3.5"`, false},
		// number is either
		{".[number]", "3", true},
		{".[number]", "3.5", true},
		{".[number]", `"3"`, false},
		// in the place a schema actually says it
		{"{age: .[int]}", "age: 3", true},
		{"{age: .[int]}", "age: 3.5", false},
		{"{age: .[int]}", "age: notanint", false},
		// and through the element type of a container, which is where the
		// ticket started: .[array(int)] accepted a list of anything
		{".[array(int)]", "- 1\n- 2", true},
		{".[array(int)]", "- 1\n- hello\n- {a: 1}", false},
		{".[array(int)]", "- 1\n- 2.5", false},
		{".[array(float)]", "- 1.5\n- 2.5", true},
		{".[array(float)]", "- 1.5\n- 2", false},
	}

	for _, test := range tests {
		t.Run(test.accept+" vs "+strings.ReplaceAll(test.doc, "\n", " "), func(t *testing.T) {
			s := baseSchema(t, "accept: "+test.accept+"\n")
			doc, err := parse.Parse([]byte(test.doc + "\n"))
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

// The satisfiability check let base.tony's own int through, and both reasons are
// guards worth keeping: it read an object pattern as a constraint on the named
// field alone rather than on the node being an object, and it read a bare null
// pattern as the null type rather than as the wildcard it is.
func TestUnsatisfiableSchemasAreRejected(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{{
		name:   "a node which must be a number and an object",
		schema: "define:\n  n: !and\n  - .[number]\n  - int: .[number]\naccept: .[n]\n",
	}, {
		name:   "!not null matches nothing, since null matches everything",
		schema: "accept: {age: !not null}\n",
	}, {
		name:   "a node which must be a string and an array",
		schema: "define:\n  n: !and\n  - .[string]\n  - [.[number]]\naccept: .[n]\n",
	}, {
		name:   "the old int, as base.tony had it",
		schema: "define:\n  myint: !and\n  - .[number]\n  - int: !not null\naccept: .[myint]\n",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(test.schema))
			if err != nil {
				t.Fatalf("parse schema: %s", err)
			}
			if err := MergeBaseDefinitions(node); err != nil {
				t.Fatalf("base definitions: %s", err)
			}
			if _, err := ParseSchema(node); err == nil {
				t.Error("a schema nothing can satisfy was accepted")
			}
		})
	}
}

// A pattern over the node is not a pattern over the document, and the checker
// does not pretend to reason about it -- but it must not invent a contradiction
// either, in either polarity.
func TestIRIsSatisfiableInBothPolarities(t *testing.T) {
	for _, src := range []string{
		"accept: !ir {int: .[number]}\n",
		"accept: !not.ir {int: .[number]}\n",
		"accept: !and [.[number], !ir {int: .[number]}]\n",
		"accept: !and [.[number], !not.ir {int: .[number]}]\n",
	} {
		node, err := parse.Parse([]byte(src))
		if err != nil {
			t.Fatalf("parse schema: %s", err)
		}
		if err := MergeBaseDefinitions(node); err != nil {
			t.Fatalf("base definitions: %s", err)
		}
		if _, err := ParseSchema(node); err != nil {
			t.Errorf("%s: %s", strings.TrimSpace(src), err)
		}
	}
}
