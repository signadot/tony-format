package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// !comment's operand may carry the VALUE as well as the positions, so a node whose
// comment and whose value both changed is one statement.
//
// It is a field of the operand rather than a second child because tag composition
// shares one child: !comment.replace hands the same operand to both, and !comment
// refuses one holding from: and to:.
//
// What needed it: a diff of two states has no other answer for such a node. Stating
// it whole works for the ordinary diff, which has !replace and carries both sides;
// an absolute delta stating an object whole MERGES, so whatever the new value no
// longer has stays (xqpvk3ehh12ks89mj5n0).
func TestCommentValue(t *testing.T) {
	tests := []struct {
		name, doc, patch, want string
		// err is a fragment of the refusal, for the cases that have one.
		err string
	}{{
		name:  "a comment and a deletion at one node",
		doc:   "a:\n  b: 1\n  c: 2\n",
		patch: `{a: !comment {head: ["# new"], value: {b: !delete null}}}`,
		want:  "a:\n  # new\n  c: 2\n",
	}, {
		// The value is applied first and the comment stated on the result: a
		// comment describes what the value has become.
		name:  "a comment and a change at one node",
		doc:   "a:\n  b: 1\n",
		patch: `{a: !comment {head: ["# new"], value: {b: 9}}}`,
		want:  "a:\n  # new\n  b: 9\n",
	}, {
		name:  "a line comment and a value",
		doc:   "a: 1\n",
		patch: `{a: !comment {line: [" # why"], value: 9}}`,
		want:  "a: 9 # why\n",
	}, {
		// Absent says nothing about the value, which is what every operand field
		// does: the head is left alone when only line: is named, and so is the
		// value when neither names it.
		name:  "no value named leaves the value alone",
		doc:   "a:\n  b: 1\n",
		patch: `{a: !comment {head: ["# new"]}}`,
		want:  "a:\n  # new\n  b: 1\n",
	}, {
		// And present says what it is. Absent is not null.
		name:  "a null value says the value is null",
		doc:   "a:\n  b: 1\n",
		patch: `{a: !comment {head: ["# new"], value: null}}`,
		want:  "# new\na: null\n",
	}, {
		// !pass is the format's "unchanged here", and it is honoured at all three.
		// value: had it for free, since that operand goes through the patch
		// function; the positions read their operand directly and used to ignore
		// the tag, so `head: !pass` CLEARED the comment -- silently, and the
		// opposite of what it said.
		name:  "!pass leaves the head comment alone",
		doc:   "a:\n  # old\n  b: 1\n",
		patch: `{a: !comment {head: !pass null}}`,
		want:  "a:\n  # old\n  b: 1\n",
	}, {
		name:  "!pass leaves the line comment alone",
		doc:   "a: 1 # latch\n",
		patch: `{a: !comment {line: !pass null}}`,
		want:  "a: 1 # latch\n",
	}, {
		name:  "!pass leaves the value alone",
		doc:   "a:\n  b: 1\n",
		patch: `{a: !comment {head: ["# new"], value: !pass null}}`,
		want:  "a:\n  # new\n  b: 1\n",
	}, {
		// The positions differ from value: here, deliberately. Setting the lines
		// to nothing is how a comment is removed, so an empty list and null both
		// say the comment is GONE -- where for the value, null says it IS null.
		name:  "null at a position removes the comment",
		doc:   "a:\n  # old\n  b: 1\n",
		patch: `{a: !comment {head: null}}`,
		want:  "a:\n  b: 1\n",
	}, {
		name:  "and so does an empty list",
		doc:   "a:\n  # old\n  b: 1\n",
		patch: `{a: !comment {head: []}}`,
		want:  "a:\n  b: 1\n",
	}, {
		name:  "an unknown field is still refused",
		doc:   "a: 1\n",
		patch: `{a: !comment {head: ["# new"], values: 9}}`,
		err:   `operand names "head", "line" or "value", got "values"`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("doc: %s", err)
			}
			patch, err := parse.Parse([]byte(test.patch), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("patch: %s", err)
			}
			got, err := Patch(doc, patch, mergeop.Comments(true))
			if test.err != "" {
				if err == nil {
					t.Fatalf("it answered %s, and should have refused", encode.MustString(got))
				}
				if !strings.Contains(err.Error(), test.err) {
					t.Errorf("refused with\n%s\nwhich does not say %q", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("patch: %s", err)
			}
			if got == nil {
				t.Fatal("the patch answered with no document")
			}
			buf := &strings.Builder{}
			if err := encode.Encode(got, buf, encode.EncodeComments(true)); err != nil {
				t.Fatalf("encode: %s", err)
			}
			if buf.String() != test.want {
				t.Errorf("wrote\n%q\nwant\n%q", buf.String(), test.want)
			}
		})
	}
}

// The same operand shape asks as a MATCH, so a pattern and the patch derived from it
// keep one shape.
func TestCommentValueMatch(t *testing.T) {
	tests := []struct {
		name, doc, pattern string
		want               bool
	}{{
		name:    "both agree",
		doc:     "a:\n  # note\n  b: 1\n",
		pattern: `{a: !comment {head: ["# note"], value: {b: 1}}}`,
		want:    true,
	}, {
		name:    "the comment agrees and the value does not",
		doc:     "a:\n  # note\n  b: 2\n",
		pattern: `{a: !comment {head: ["# note"], value: {b: 1}}}`,
	}, {
		name:    "the value agrees and the comment does not",
		doc:     "a:\n  # other\n  b: 1\n",
		pattern: `{a: !comment {head: ["# note"], value: {b: 1}}}`,
	}, {
		// A position named as unchanged is not asked about, the same as one not
		// named at all.
		name:    "!pass at a position asks nothing of it",
		doc:     "a:\n  # anything\n  b: 1\n",
		pattern: `{a: !comment {head: !pass null, value: {b: 1}}}`,
		want:    true,
	}, {
		// Unchanged: with no value named it asks about the comments only.
		name:    "no value named asks nothing of the value",
		doc:     "a:\n  # note\n  b: 99\n",
		pattern: `{a: !comment {head: ["# note"]}}`,
		want:    true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("doc: %s", err)
			}
			pat, err := parse.Parse([]byte(test.pattern), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("pattern: %s", err)
			}
			got, err := Match(doc, pat)
			if err != nil {
				t.Fatalf("match: %s", err)
			}
			if got != test.want {
				t.Errorf("answered %v, want %v", got, test.want)
			}
		})
	}
}
