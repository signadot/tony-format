package eval

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The environment of xxn12a3gh12krfg8x5n0: a subject with a value, and under it a
// scalar, a number, a list, an object and a written null.
func optionsEnv() map[string]any {
	return map[string]any{"subject": map[string]any{"name": "a.b", "value": map[string]any{
		"sev": "high", "n": 3.0, "items": []any{1.0, 2.0}, "obj": map[string]any{"x": 1.0}, "nul": nil,
	}}}
}

// An expression ends at the ] that closes it, so an index or a quoted ] needs no
// escape; outside an expression $$[ is the one literal $[ (xxn12a3gh12krfg8x5n0).
func TestExpressionEnds(t *testing.T) {
	env := optionsEnv()
	for _, tc := range []envTest{
		{in: "$[subject.value.items[0]]", out: "1"},
		{in: `$[subject.value["obj"]["x"]]`, out: "1"},
		{in: `$["a]b"]`, out: "a]b"},
		{in: `$['a]b']`, out: "a]b"},
		{in: "$[subject.value.items[0:1]]", out: "[1]"},
		{in: "$[subject.value.items[0]", out: "$[subject.value.items[0]"},
		{in: "literal $$[x]", out: "literal $[x]"},
		{in: "$$HOME and $HOME", out: "$$HOME and $HOME"},
		{in: "a $$[ b", out: "a $[ b"},
		{in: "$[subject.value.sev] $$[x] $[subject.value.n]", out: "high $[x] 3"},
	} {
		got, err := ExpandString(tc.in, env)
		if err != nil || got != tc.out {
			t.Errorf("%s: %q, %v; want %q", tc.in, got, err, tc.out)
		}
	}
	// A quote nothing closes does not take the ] with it: the expression ends at the
	// first ] as it always did, and the bad string is refused rather than passed
	// through as text.
	if got, err := ExpandString(`$[upper("abc] tail`, env); err == nil {
		t.Errorf("an unclosed quote: %q, want an error", got)
	}
}

// A composite is written into text as the embedder says: its JSON, its tony on one
// line, or not at all. A reference is its value whatever that says.
func TestComposite(t *testing.T) {
	env := optionsEnv()
	for _, tc := range []struct {
		form CompositeForm
		in   string
		want string
		err  string
	}{
		{CompositeJSON, "$[subject.value.obj]", `{"x":1}`, ""},
		{CompositeJSON, "$[subject.value.items]", "[1,2]", ""},
		{CompositeTony, "$[subject.value.obj]", "{x: 1}", ""},
		{CompositeTony, "$[subject.value.items]", "[1 2]", ""},
		{CompositeTony, "$[[1, {a: [2]}]]", "[1 {a: [2]}]", ""},
		{CompositeTony, "$[subject.value.sev]", "high", ""},
		{CompositeRefuse, "$[subject.value.obj]", "", "subject.value.obj: an object has no text form"},
		{CompositeRefuse, "$[subject.value.items]", "", "subject.value.items: a list has no text form"},
		{CompositeRefuse, "$[subject.value.sev]", "high", ""},
		{CompositeRefuse, "$[subject.value.nul]", "null", ""},
	} {
		got, err := ExpandStringWithOptions(tc.in, env, &EvalOptions{Composite: tc.form})
		switch {
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("%v %s: %q, %v; want an error mentioning %q", tc.form, tc.in, got, err, tc.err)
		case tc.err == "" && (err != nil || got != tc.want):
			t.Errorf("%v %s: %q, %v; want %q", tc.form, tc.in, got, err, tc.want)
		}
	}
	n, err := ExpandIRWithOptions(ir.FromString(".[subject.value.obj]"), env, &EvalOptions{Composite: CompositeRefuse})
	if err != nil || n == nil || n.Type != ir.ObjectType {
		t.Errorf("a reference to an object under CompositeRefuse: %v, %v; want the object", n, err)
	}
}

// A disabled function is refused by name, expr-lang's builtins and this package's
// alike, and a builtin that takes a predicate among them; the rest stay.
func TestDisableBuiltins(t *testing.T) {
	env := optionsEnv()
	opts := &EvalOptions{DisableBuiltins: []string{"now", "fail", "map"}}
	for _, tc := range []struct{ in, want string }{
		{"$[now()]", "now() is not available here"},
		{`$[fail("x")]`, "fail() is not available here"},
		{"$[map(subject.value.items, # + 1)]", "map() is not available here"},
	} {
		_, err := ExpandStringWithOptions(tc.in, env, opts)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want an error mentioning %q", tc.in, err, tc.want)
		}
	}
	if got, err := ExpandStringWithOptions("$[upper(subject.value.sev)]", env, opts); err != nil || got != "HIGH" {
		t.Errorf("upper with now disabled: %q, %v; want HIGH", got, err)
	}
	if _, err := ExpandString("$[now()]", env); err != nil {
		t.Errorf("now without the option: %v", err)
	}
	// The options reach a line comment too.
	doc, err := parse.Parse([]byte("x: 1 # built $[now()]\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExpandIRWithOptions(doc, env, opts); err == nil || !strings.Contains(err.Error(), "now() is not available here") {
		t.Errorf("now in a line comment: %v, want it refused", err)
	}
}

// getpath answers a typed nil for a path the document does not hold, and written into
// text it is null, as it is as a reference.
func TestAbsentPathInText(t *testing.T) {
	doc, err := parse.Parse([]byte("a: 1\nb: \"got $[getpath(\\\"$.zz\\\")]\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ExpandIR(doc, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := out.GetKPath("b"); b == nil || b.String != "got null" {
		t.Errorf("b is %v, want \"got null\"", b)
	}
}
