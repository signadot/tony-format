package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
)

// !get-path answers with the value at a path, which nothing else in the
// vocabulary could say: !at walks to a path and applies a match there, !embed
// hands over the whole node with no path, !has-path answers whether.
func TestGetPathPatches(t *testing.T) {
	const doc = `{spec: {image: "app:1.4", replicas: 3}, status: {replicas: 3}, containers: [{image: a}, {image: b}]}`

	for _, tc := range []struct {
		name, patch, want string
	}{{
		name:  "into a field the document has",
		patch: `{status: {replicas: !get-path(root) spec.replicas}}`,
		want:  `3`,
	}, {
		// The case the operator is mostly FOR, and the one the patch engine had
		// to be taught: a field the document does not have is patched against a
		// placeholder standing where it would be, so (root) reaches the document
		// from a position which holds nothing yet.
		name:  "into a field the document does not have",
		patch: `{status: {image: !get-path(root) spec.image}}`,
		want:  `"app:1.4"`,
	}, {
		name:  "into a path which is created all the way down",
		patch: `{brand: {new: {deep: !get-path(root) spec.image}}}`,
		want:  `"app:1.4"`,
	}, {
		// Relative means BELOW the node the operator is written at, so this one
		// reads spec.image and the answer replaces spec.
		name:  "relative to the node it is written at",
		patch: `{spec: !get-path image}`,
		want:  `"app:1.4"`,
	}, {
		name:  "a whole subtree",
		patch: `{copy: !get-path(root) spec}`,
		want:  `{image: "app:1.4", replicas: 3}`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Patch(mustParseNode(t, doc), mustParseNode(t, tc.patch))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			at := firstLeafPath(t, tc.patch)
			node, err := got.GetPath(at)
			if err != nil || node == nil {
				t.Fatalf("%s is not in the result: %v\n%s", at, err, encode.MustString(got))
			}
			if out, want := encode.MustString(node), encode.MustString(mustParseNode(t, tc.want)); out != want {
				t.Errorf("%s:\ngot  %s\nwant %s", at, out, want)
			}
		})
	}
}

// An element written past the end of the document's array meets the same
// placeholder as an absent field.
func TestGetPathIntoAnArrayTail(t *testing.T) {
	got, err := tony.Patch(
		mustParseNode(t, `{v: hello, xs: [1]}`),
		mustParseNode(t, `{xs: [1, !get-path(root) v]}`))
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	// by value: a patch which extends an array indents its result its own way,
	// which is nothing to do with where the element came from
	tail, err := got.GetPath("$.xs[1]")
	if err != nil || tail == nil {
		t.Fatalf("xs[1] is not in the result: %v\n%s", err, encode.MustString(got))
	}
	if tail.String != "hello" {
		t.Errorf("xs[1] = %q, want %q", tail.String, "hello")
	}
}

// !get-paths is the plural, and takes the paths its singular refuses.
func TestGetPathsAnswersAList(t *testing.T) {
	const doc = `{containers: [{image: a}, {image: b}], one: {image: c}}`

	for _, tc := range []struct {
		name, patch, want string
	}{{
		name:  "a wild path",
		patch: `{all: !get-paths(root) "containers[*].image"}`,
		want:  `[a, b]`,
	}, {
		// no special case: a path naming one node gives a list of one
		name:  "a path which names one node",
		patch: `{all: !get-paths(root) one.image}`,
		want:  `[c]`,
	}, {
		// each keeps the promise its name makes: !get-path errors here
		name:  "a path which names nothing",
		patch: `{all: !get-paths(root) "nope[*]"}`,
		want:  `[]`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Patch(mustParseNode(t, doc), mustParseNode(t, tc.patch))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			all, err := got.GetPath("$.all")
			if err != nil || all == nil {
				t.Fatalf("all is not in the result: %v", err)
			}
			// element by element: a parsed [a, b] carries a bracket tag and the
			// built list does not, which is presentation and not the answer
			want := mustParseNode(t, tc.want)
			if len(all.Values) != len(want.Values) {
				t.Fatalf("got %d values, want %d: %s", len(all.Values), len(want.Values), encode.MustString(all))
			}
			for i := range want.Values {
				if got, wants := encode.MustString(all.Values[i]), encode.MustString(want.Values[i]); got != wants {
					t.Errorf("[%d] = %s, want %s", i, got, wants)
				}
			}
		})
	}
}

// A relation between two parts of one document, which no pattern could state.
func TestGetPathMatches(t *testing.T) {
	for _, tc := range []struct {
		name, doc, pattern string
		want               bool
	}{{
		name:    "two fields which agree",
		doc:     `{spec: {replicas: 3}, status: {replicas: 3}}`,
		pattern: `{status: {replicas: !get-path(root) spec.replicas}}`,
		want:    true,
	}, {
		name:    "and two which do not",
		doc:     `{spec: {replicas: 3}, status: {replicas: 1}}`,
		pattern: `{status: {replicas: !get-path(root) spec.replicas}}`,
		want:    false,
	}, {
		// the operator is at `a`, so the path reads a.inner, and the answer is
		// matched against a itself
		name:    "relative to the node it is written at",
		doc:     `{a: {inner: {v: 1}, v: 1}}`,
		pattern: `{a: !get-path inner}`,
		want:    true,
	}, {
		name:    "and can fail there",
		doc:     `{a: {inner: {v: 1}, v: 2}}`,
		pattern: `{a: !get-path inner}`,
		want:    false,
	}, {
		name:    "composed",
		doc:     `{spec: {replicas: 3}, status: {replicas: 1}}`,
		pattern: `{status: {replicas: !not.get-path(root) spec.replicas}}`,
		want:    true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Match(mustParseNode(t, tc.doc), mustParseNode(t, tc.pattern))
			if err != nil {
				t.Fatalf("match: %v", err)
			}
			if got != tc.want {
				t.Errorf("matched=%v, want %v", got, tc.want)
			}
		})
	}
}

// A path naming nothing is an error rather than a null or a silent no-match:
// this operator answers with a VALUE, and there is none. That departs from !at,
// which reads the same absence as a mismatch -- !at relocates a PATTERN, so a
// document without an a.b is a document which fails it.
func TestGetPathRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, want string
	}{{
		name:    "a path which names nothing",
		pattern: `{x: !get-path(root) nope.here}`,
		want:    "the path names nothing in the document",
	}, {
		name:    "a relative path which names nothing",
		pattern: `{x: !get-path nope}`,
		want:    "names nothing below the node it is written at",
	}, {
		name:    "a path which runs into a scalar",
		pattern: `{x: !get-path(root) a.b.deeper}`,
		want:    "names nothing in the document",
	}, {
		name:    "a wild path, which names a set rather than a node",
		pattern: `{x: !get-path(root) "a[*]"}`,
		want:    "names a set of nodes rather than one; get-paths answers a list",
	}, {
		name:    "a path which does not parse",
		pattern: `{x: !get-path "a["}`,
		want:    "get-path a[",
	}, {
		name:    "an operand which is not a path",
		pattern: `{x: !get-path 3}`,
		want:    "expects a kpath as its operand, got Number",
	}, {
		name:    "a tag component which is not the anchor",
		pattern: `{x: !get-path(bogus) a}`,
		want:    `the only tag component is "root"`,
	}, {
		name:    "and the plural refuses the same things",
		pattern: `{x: !get-paths(bogus) a}`,
		want:    `the only tag component is "root"`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			// x is in the document so the operator is reached: a pattern field the
			// document does not have is absent, and absent never runs the op.
			doc := mustParseNode(t, `{a: {b: 1}, x: 0}`)
			pat := mustParseNode(t, tc.pattern)

			got, err := tony.Patch(doc, pat)
			if err == nil {
				t.Fatalf("patch: no error, result %s; want one saying %q", encode.MustString(got), tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("patch: error %q does not say %q", err, tc.want)
			}
			if _, err := tony.Match(doc, pat); err == nil {
				t.Errorf("match: no error; want one saying %q", tc.want)
			}
		})
	}
}

// The value the operator hands over is bound to a name once and written wherever
// the body says, which is what a binding is for and what this started from.
func TestGetPathUnderLet(t *testing.T) {
	got, err := tony.Patch(
		mustParseNode(t, `{spec: {image: "app:1.4"}}`),
		mustParseNode(t, `!let {let: [{img: !get-path(root) spec.image}], in: {main: {image: .[img]}, side: {image: .[img]}}}`))
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	for _, path := range []string{"$.main.image", "$.side.image"} {
		node, err := got.GetPath(path)
		if err != nil || node == nil {
			t.Fatalf("%s is not in the result: %v\n%s", path, err, encode.MustString(got))
		}
		if node.String != "app:1.4" {
			t.Errorf("%s = %q, want %q", path, node.String, "app:1.4")
		}
	}
}

// firstLeafPath is the objpath of the one leaf a single-branch patch writes.
func firstLeafPath(t *testing.T, patch string) string {
	t.Helper()
	n := mustParseNode(t, patch)
	path := "$"
	for n != nil && len(n.Fields) == 1 {
		path += "." + n.Fields[0].String
		n = n.Values[0]
	}
	return path
}
