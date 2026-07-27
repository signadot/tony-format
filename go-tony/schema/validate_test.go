package schema

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/parse"
)

// answerContract is an answer contract in the sense of issue
// axp74f1wh12krznjcxn0: a schema an agent is told to answer in, whose accept
// clause a loop validates the answer against.  What comes back from a
// rejection has to be specific enough to repair.
const answerContract = `
signature: {name: answer}
define: {sev: !or [low, high]}
accept: {class: !or [bug, nit, risk], severity: .[sev], why: !irtype string}
`

func TestValidateSaysWhy(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		// fails is one "path|op|reason" per expected failure, in order.
		fails []string
	}{{
		name: "ok",
		doc:  `{class: bug, severity: high, why: "nil deref"}`,
	}, {
		// a document carrying more than it was asked for is still an answer
		name: "extra-field",
		doc:  `{class: bug, severity: high, why: "x", note: chatty}`,
	}, {
		name:  "bad-enum",
		doc:   `{class: critical, severity: high, why: "nil deref"}`,
		fails: []string{"class|or|op"},
	}, {
		name:  "missing-field",
		doc:   `{class: bug, why: "nil deref"}`,
		fails: []string{"severity||absent"},
	}, {
		name:  "wrong-type",
		doc:   `{class: bug, severity: high, why: 3}`,
		fails: []string{"why|irtype|op"},
	}, {
		// one round trip should be able to repair both
		name:  "two-failures",
		doc:   `{class: critical, why: "nil deref"}`,
		fails: []string{"class|or|op", "severity||absent"},
	}, {
		// the failure is inside a definition the accept clause referred to
		name:  "bad-enum-through-a-ref",
		doc:   `{class: bug, severity: urgent, why: "nil deref"}`,
		fails: []string{"severity|or|op"},
	}}

	s := mustSchema(t, answerContract)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc))
			if err != nil {
				t.Fatalf("parse: %s", err)
			}
			err = s.Validate(doc)
			if len(test.fails) == 0 {
				if err != nil {
					t.Fatalf("accepted document rejected: %s", err)
				}
				return
			}
			if err == nil {
				t.Fatal("rejected document accepted")
			}
			var invalid *ValidationError
			if !errors.As(err, &invalid) {
				t.Fatalf("error is %T: %s", err, err)
			}
			var got []string
			for _, f := range invalid.Explanation.Failures {
				got = append(got, fmt.Sprintf("%s|%s|%s", f.Path, f.Op, f.Reason))
			}
			if strings.Join(got, ",") != strings.Join(test.fails, ",") {
				t.Errorf("failures\n\tgot  %v\n\twant %v", got, test.fails)
			}
			// the reasons are in the message a caller feeds back
			for _, f := range invalid.Explanation.Failures {
				if !strings.Contains(err.Error(), f.Path) {
					t.Errorf("message does not name %q:\n%s", f.Path, err)
				}
			}
		})
	}
}

// TestValidateAbsentIsNotWrongType: the distinction a caller can most often
// repair by itself must survive to the caller.
func TestValidateAbsentIsNotWrongType(t *testing.T) {
	s := mustSchema(t, answerContract)
	reason := func(doc string) tony.Reason {
		t.Helper()
		node, err := parse.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("parse: %s", err)
		}
		var invalid *ValidationError
		if !errors.As(s.Validate(node), &invalid) {
			t.Fatalf("%s: not rejected", doc)
		}
		return invalid.Explanation.Failures[0].Reason
	}
	absent := reason(`{class: bug, why: "nil deref"}`)
	wrong := reason(`{class: bug, severity: 3, why: "nil deref"}`)
	if absent != tony.ReasonAbsent {
		t.Errorf("absent field reported as %s", absent)
	}
	if wrong == absent {
		t.Errorf("present-but-wrong reported as %s, same as absent", wrong)
	}
}

func mustSchema(t *testing.T, src string) *Schema {
	t.Helper()
	node, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse schema: %s", err)
	}
	s, err := ParseSchema(node)
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	return s
}
