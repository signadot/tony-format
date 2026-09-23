package eval

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// TestFailRaisesTheAuthorsMessage: an expression had no way to say "this should not
// have happened". The spellings that read like one -- fail, error, panic -- were each
// "cannot call nil", so the only way to refuse was to fetch through something known to
// be absent, which reads as a mistake rather than an intention (9mt3aenph12krmc1pdn0).
func TestFailRaisesTheAuthorsMessage(t *testing.T) {
	env := Env{"subject": map[string]any{"value": map[string]any{"sha": "abc"}}}
	_, err := ExpandString(`$[fail("no sha")]`, env)
	if err == nil || !strings.Contains(err.Error(), "no sha") {
		t.Fatalf("fail answered %v, and the author's message is not in it", err)
	}
}

// TestFailIsTheElseOfATernary is what it is for: the value when there is one, the
// author's refusal when there is not. A ternary short-circuits, so the branch not
// taken raises nothing.
func TestFailIsTheElseOfATernary(t *testing.T) {
	const e = `$["sha" in subject.value ? subject.value.sha : fail("no sha in the subject")]`

	got, err := ExpandString(e, Env{"subject": map[string]any{"value": map[string]any{"sha": "abc"}}})
	if err != nil {
		t.Fatalf("the branch not taken raised: %v", err)
	}
	if got != "abc" {
		t.Errorf("answered %q, want abc", got)
	}

	_, err = ExpandString(e, Env{"subject": map[string]any{"value": map[string]any{}}})
	if err == nil || !strings.Contains(err.Error(), "no sha in the subject") {
		t.Fatalf("a subject without a sha answered %v", err)
	}
}

// TestFailIsThereWithoutADocument: fail needs nothing of the document, so it is in
// baseOpts and not exprOpts -- an expansion given no node, which is what a caller
// embedding eval does (ExpandString), has it. getpath and the rest resolve THROUGH a
// node and are only there when one is given.
func TestFailIsThereWithoutADocument(t *testing.T) {
	if _, err := ExpandString(`$[fail("x")]`, Env{}); err == nil || !strings.Contains(err.Error(), "x") {
		t.Errorf("without a node, fail answered %v", err)
	}
	node := &ir.Node{Type: ir.StringType, String: `.[fail("y")]`}
	if err := ExpandEnv(node, Env{}); err == nil || !strings.Contains(err.Error(), "y") {
		t.Errorf("with a node, fail answered %v", err)
	}
}

// TestWhatAnAbsentPathAnswers states what the doc states: a fetch says whether what it
// fetches from must be there. "." demands it, "?." tolerates it, "??" defaults a null
// but cannot rescue a fetch on nothing, and "in" tells absent from written null. The
// first two rows are the unevenness reported in 9mt3aenph12krmc1pdn0: one missing name
// is null and two is an error. It is not a rule about paths, it is the fetch ON
// nothing that fails, and an author who wants one answer however far the path got
// writes "?.".
func TestWhatAnAbsentPathAnswers(t *testing.T) {
	const present, missing = true, false
	for _, c := range []struct {
		expr   string
		parent bool
		want   string // the expansion, or "!" followed by what the error says
	}{
		{`$[subject.value.nope]`, present, "null"},
		{`$[subject.value.nope.deeper]`, present, "!cannot fetch deeper from <nil>"},
		{`$[subject.value.sha]`, present, "abc"},
		{`$[subject.value.sha ?? "unset"]`, present, "abc"},
		{`$[subject.value.nope ?? "unset"]`, present, "unset"},
		{`$[subject.value?.nope?.deeper ?? "unset"]`, present, "unset"},
		{`$["sha" in subject.value]`, present, "true"},
		{`$["nope" in subject.value]`, present, "false"},
		{`$[subject.value.nul == nil]`, present, "true"},
		{`$["nul" in subject.value]`, present, "true"},

		{`$[subject.value.sha]`, missing, "!cannot fetch sha from <nil>"},
		{`$[subject.value.sha ?? "unset"]`, missing, "!cannot fetch sha from <nil>"},
		{`$[subject.value?.sha ?? "unset"]`, missing, "unset"},
		{`$[subject.value?.nope?.deeper ?? "unset"]`, missing, "unset"},
		{`$["sha" in subject.value]`, missing, "false"},
	} {
		var value any
		if c.parent {
			value = map[string]any{"sha": "abc", "nul": nil}
		}
		got, err := ExpandString(c.expr, Env{"subject": map[string]any{"value": value}})
		if want, isErr := strings.CutPrefix(c.want, "!"); isErr {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s (parent %v): answered %q, %v; want the error %q", c.expr, c.parent, got, err, want)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s (parent %v): %v", c.expr, c.parent, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s (parent %v): answered %q, want %q", c.expr, c.parent, got, c.want)
		}
	}
}
