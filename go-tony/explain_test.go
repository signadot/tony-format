package tony

import (
	"fmt"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

// answer is the shape of an answer contract: a few enumerated fields, a typed
// one, and nothing said about anything else.
const answer = `
class: !or [bug, nit, risk]
severity: !or [low, high]
why: !irtype string
`

type explainTest struct {
	name string
	doc  string
	pat  string
	// fails is one "path|op|reason" per expected failure, in order.
	fails []string
}

var explainTests = []explainTest{{
	name: "ok",
	doc:  `{class: bug, severity: high, why: "nil deref"}`,
	pat:  answer,
}, {
	name: "extra-field",
	doc:  `{class: bug, severity: high, why: "x", note: chatty}`,
	pat:  answer,
}, {
	name:  "bad-enum",
	doc:   `{class: critical, severity: high, why: "nil deref"}`,
	pat:   answer,
	fails: []string{"class|or|op"},
}, {
	name:  "missing-field",
	doc:   `{class: bug, why: "nil deref"}`,
	pat:   answer,
	fails: []string{"severity||absent"},
}, {
	name:  "wrong-type",
	doc:   `{class: bug, severity: high, why: 3}`,
	pat:   answer,
	fails: []string{"why|irtype|op"},
}, {
	name:  "two-failures",
	doc:   `{class: critical, why: "nil deref"}`,
	pat:   answer,
	fails: []string{"class|or|op", "severity||absent"},
}, {
	name:  "all-names-the-element",
	doc:   `{findings: [{severity: low}, {severity: bogus}, {severity: high}]}`,
	pat:   `{findings: !all {severity: !or [low, high]}}`,
	fails: []string{"findings[1].severity|or|op"},
}, {
	name:  "nested-path",
	doc:   `{a: {b: [1, {c: 2}]}}`,
	pat:   `{a: {b: [1, {c: 3}]}}`,
	fails: []string{"a.b[1].c||value"},
}, {
	name:  "quoted-field",
	doc:   `{"a.b": 1}`,
	pat:   `{"a.b": 2}`,
	fails: []string{`"a.b"||value`},
}, {
	name:  "absent-quoted-field",
	doc:   `{x: 1}`,
	pat:   `{"a b": 2}`,
	fails: []string{`"a b"||absent`},
}, {
	name:  "type",
	doc:   `{a: {b: 1}}`,
	pat:   `{a: hello}`,
	fails: []string{"a||type"},
}, {
	name:  "array-length",
	doc:   `{a: [1, 2, 3]}`,
	pat:   `{a: [1, 2]}`,
	fails: []string{"a||length"},
}, {
	name:  "every-bad-element",
	doc:   `[1, 2, 3]`,
	pat:   `[1, 9, 8]`,
	fails: []string{"[1]||value", "[2]||value"},
}, {
	name:  "not",
	doc:   `{a: 1}`,
	pat:   `{a: !not 1}`,
	fails: []string{"a|not|op"},
}, {
	name:  "and-stops-at-its-first",
	doc:   `{a: hello}`,
	pat:   `{a: !and [!irtype 3, !glob "w*"]}`,
	fails: []string{"a|irtype|op"},
}, {
	name:  "root",
	doc:   `1`,
	pat:   `2`,
	fails: []string{"||value"},
}, {
	name:  "field-name",
	doc:   `{a: 1}`,
	pat:   `{a: !field b}`,
	fails: []string{"a|field|op"},
}}

func TestExplain(t *testing.T) {
	for _, test := range explainTests {
		t.Run(test.name, func(t *testing.T) {
			doc, pat := mustParse(t, test.doc), mustParse(t, test.pat)
			var why Explanation
			matched, err := Match(doc, pat, Explaining(&why))
			if err != nil {
				t.Fatalf("match: %s", err)
			}
			if matched != (len(test.fails) == 0) {
				t.Fatalf("matched %v, want %v", matched, !matched)
			}
			if why.Matched != matched {
				t.Errorf("why.Matched %v, match returned %v", why.Matched, matched)
			}
			if matched {
				if why.Root != nil {
					t.Errorf("a match which succeeded recorded %v", why.Root)
				}
				return
			}
			got := summarize(why.Failures)
			if !equal(got, test.fails) {
				t.Errorf("failures\n\tgot  %v\n\twant %v\n%s", got, test.fails, &why)
			}
			for _, f := range why.Failures {
				if f.Reason != ReasonAbsent && f.Found == nil {
					t.Errorf("%s: no node found", f.Path)
				}
				if f.Expected == nil {
					t.Errorf("%s: nothing expected", f.Path)
				}
			}
		})
	}
}

// TestExplainSaysWhichNode is the point of the paths: the node a failure
// names must be the node which failed, reachable in the document by that
// path.
func TestExplainSaysWhichNode(t *testing.T) {
	doc := mustParse(t, `{findings: [{severity: low}, {severity: bogus}]}`)
	pat := mustParse(t, `{findings: !all {severity: !or [low, high]}}`)
	var why Explanation
	if matched, err := Match(doc, pat, Explaining(&why)); err != nil || matched {
		t.Fatalf("match %v, %v", matched, err)
	}
	f := why.Failures[0]
	at, err := doc.GetKPath(f.Path)
	if err != nil {
		t.Fatalf("get %q: %s", f.Path, err)
	}
	if encode.MustString(at) != encode.MustString(f.Found) {
		t.Errorf("%q reaches %s, failure found %s", f.Path,
			encode.MustString(at), encode.MustString(f.Found))
	}
	// Found is the document node itself, not a copy of it
	if f.Found != doc.Values[0].Values[1].Values[0] {
		t.Errorf("found node is not the node in the document")
	}
	if got := encode.MustString(f.Found); got != "bogus" {
		t.Errorf("found %q, want bogus", got)
	}
	// the alternatives are readable from the pattern node, unrendered
	if got := encode.MustString(f.Expected); !strings.Contains(got, "high") {
		t.Errorf("expected %q, want the whole !or", got)
	}
}

// TestExplainMalformedPattern: a pattern which is not a pattern is an error,
// not a mismatch, and the explanation says where it is.
func TestExplainMalformedPattern(t *testing.T) {
	doc := mustParse(t, `{a: {b: 1}}`)
	pat := mustParse(t, `{a: {b: !or(nope) [1, 2]}}`)
	var why Explanation
	matched, err := Match(doc, pat, Explaining(&why))
	if err == nil {
		t.Fatalf("matched %v with no error", matched)
	}
	if len(why.Failures) != 1 {
		t.Fatalf("failures %v", summarize(why.Failures))
	}
	f := why.Failures[0]
	if f.Reason != ReasonError || f.Path != "a.b" || f.Err == nil {
		t.Errorf("got %s %s %v", f.Path, f.Reason, f.Err)
	}
}

// TestTraceWhichBranchMatched: the positive polarity, which is what makes a
// rule debuggable -- a trigger which fired says why it fired.
func TestTraceWhichBranchMatched(t *testing.T) {
	doc := mustParse(t, `{class: nit, findings: [{severity: low}]}`)
	pat := mustParse(t, `{class: !or [bug, nit, risk], findings: !all {severity: !or [low, high]}}`)
	var why Explanation
	matched, err := Match(doc, pat, Tracing(&why))
	if err != nil || !matched {
		t.Fatalf("match %v, %v", matched, err)
	}
	if why.Root == nil {
		t.Fatal("a trace kept nothing")
	}
	got := summarize(why.Matches)
	want := []string{"class|or|unmatched", "findings|all|unmatched", "findings[0].severity|or|unmatched"}
	if !equal(got, want) {
		t.Fatalf("matches\n\tgot  %v\n\twant %v", got, want)
	}
	// which alternative of the !or matched is under it
	or := why.Matches[0]
	var branch string
	for _, c := range or.Causes {
		if c.Matched {
			branch = encode.MustString(c.Expected)
		}
	}
	if branch != "nit" {
		t.Errorf("matched branch %q, want nit", branch)
	}
}

// TestTraceFailureIsTheSame: keeping what matched must not change which
// failures are reported.
func TestTraceFailureIsTheSame(t *testing.T) {
	for _, test := range explainTests {
		doc, pat := mustParse(t, test.doc), mustParse(t, test.pat)
		var why Explanation
		if _, err := Match(doc, pat, Tracing(&why)); err != nil {
			t.Fatalf("%s: %s", test.name, err)
		}
		if got := summarize(why.Failures); !equal(got, test.fails) {
			t.Errorf("%s: traced failures\n\tgot  %v\n\twant %v", test.name, got, test.fails)
		}
	}
}

// TestExplainDoesNotChangeTheVerdict: an explanation is a channel of its own;
// asking for one must not change the answer.
func TestExplainDoesNotChangeTheVerdict(t *testing.T) {
	for _, test := range matchTests {
		doc, pat := mustParse(t, test.in), mustParse(t, test.match)
		plain, plainErr := Match(doc, pat)
		var why Explanation
		explained, err := Match(doc, pat, Explaining(&why))
		if explained != plain || (err == nil) != (plainErr == nil) {
			t.Errorf("%q vs %q: plain %v/%v, explained %v/%v",
				test.in, test.match, plain, plainErr, explained, err)
		}
		if plainErr == nil && !plain && len(why.Failures) == 0 {
			t.Errorf("%q vs %q: no failure said why", test.in, test.match)
		}
	}
}

// TestExplainDoesNotInventErrors: a match which stops at the first failure
// never reaches the rest of the pattern.  Walking on to collect the other
// failures must not report an error the match itself would never have found.
func TestExplainDoesNotInventErrors(t *testing.T) {
	doc := mustParse(t, `{a: 1, b: 2}`)
	pat := mustParse(t, `{a: 2, b: !or(nope) [1, 2]}`)
	if plain, err := Match(doc, pat); plain || err != nil {
		t.Fatalf("plain match %v, %v", plain, err)
	}
	var why Explanation
	if explained, err := Match(doc, pat, Explaining(&why)); explained || err != nil {
		t.Fatalf("explained match %v, %v", explained, err)
	}
	got := summarize(why.Failures)
	want := []string{"a||value", "b|or|error"}
	if !equal(got, want) {
		t.Errorf("failures\n\tgot  %v\n\twant %v", got, want)
	}
}

func TestExplanationString(t *testing.T) {
	doc := mustParse(t, `{class: critical, why: "nil deref"}`)
	var why Explanation
	if _, err := Match(doc, mustParse(t, answer), Explaining(&why)); err != nil {
		t.Fatal(err)
	}
	got := why.String()
	want := "class: or: expected !or [bug nit risk], found critical\n" +
		"severity: absent: expected !or [low high]"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// TestExplanationStringBounded: Found can be a whole document, and an
// explanation which goes into a prompt has to be worth its tokens.
func TestExplanationStringBounded(t *testing.T) {
	big := make([]string, 200)
	for i := range big {
		big[i] = fmt.Sprintf("f%d: %d", i, i)
	}
	doc := mustParse(t, "{"+strings.Join(big, ", ")+"}")
	var why Explanation
	if _, err := Match(doc, mustParse(t, `hello`), Explaining(&why)); err != nil {
		t.Fatal(err)
	}
	if n := len(why.String()); n > 2*briefMax {
		t.Errorf("string is %d bytes", n)
	}
}

func summarize(frames []*Frame) []string {
	res := make([]string, 0, len(frames))
	for _, f := range frames {
		res = append(res, fmt.Sprintf("%s|%s|%s", f.Path, f.Op, f.Reason))
	}
	return res
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
