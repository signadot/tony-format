package eval

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A plain path is strict: an absent name, field or index is an error that says what
// is absent, in text and as a reference alike. A written null is null, ?. and ?? keep
// their meaning, and an expression that is not a plain path is as lenient as it was
// (xxn12a3gh12krfg8x5n0).
func TestPlainPathIsStrict(t *testing.T) {
	env := optionsEnv()
	for _, tc := range []struct{ in, want string }{
		// absent
		{"$[nosuch]", `no such name "nosuch"`},
		{"$[subject.value.never]", `no "never" in subject.value`},
		{"$[subject.value.never.x]", `no "never" in subject.value`},
		{`$[subject.value["never"]]`, `no "never" in subject.value`},
		{"$[subject.value.items[5]]", "no index 5 in subject.value.items, which has 2 elements"},
		{"$[subject.value.items[-3]]", "no index -3 in subject.value.items, which has 2 elements"},
		// a fetch on a number, and on a written null: the evaluator's errors
		{"$[subject.value.obj.x.y]", "cannot fetch y from float64"},
		{"$[subject.value.nul.x]", "cannot fetch x from <nil>"},
		// present
		{"$[subject.value.nul]", "!null"},
		{"$[subject.value.items[-1]]", "!2"},
		{"$[subject.value.obj.x]", "!1"},
		// not a plain path: lenient as before
		{"$[subject.value?.never]", "!null"},
		{`$[subject.value.never ?? "d"]`, "!d"},
		{`$["never" in subject.value]`, "!false"},
		{`$[subject.value.never == "x"]`, "!false"},
		{`$[nosuch ?? "d"]`, "!d"},
	} {
		got, err := ExpandString(tc.in, env)
		if want, ok := strings.CutPrefix(tc.want, "!"); ok {
			if err != nil || got != want {
				t.Errorf("%s: %q, %v; want %q", tc.in, got, err, want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %q, %v; want an error mentioning %q", tc.in, got, err, tc.want)
		}
	}
	if _, err := ExpandString("$[x]", nil); err == nil || !strings.Contains(err.Error(), `no such name "x"`) {
		t.Errorf("a name with no environment at all: %v, want it refused", err)
	}
}

// A reference is as strict as text, by each road it takes: ExpandIR, which a !let and
// a schema go by, and ExpandEnv, which !eval goes by.
func TestPlainPathReferenceIsStrict(t *testing.T) {
	env := optionsEnv()
	const want = `no "never" in subject.value`
	if _, err := ExpandIR(ir.FromString(".[subject.value.never]"), env); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ExpandIR of a reference to an absent field: %v, want it refused", err)
	}
	doc, err := parse.Parse([]byte("a: .[subject.value.never]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ExpandEnv(doc, env); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("ExpandEnv of a reference to an absent field: %v, want it refused", err)
	}
	doc, err = parse.Parse([]byte("a: .[subject.value.sev]\nb: \"$[subject.value.items[1]]\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ExpandEnv(doc, env); err != nil {
		t.Fatalf("ExpandEnv of present paths: %v", err)
	}
	if a, _ := doc.GetKPath("a"); a == nil || a.String != "high" {
		t.Errorf("a is %v, want high", a)
	}
	if b, _ := doc.GetKPath("b"); b == nil || b.String != "2" {
		t.Errorf("b is %v, want 2", b)
	}
}
