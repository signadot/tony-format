package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

func mustParse(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

// TestRawPatchStoresOperatorsAsData covers the cases from issue
// 7f8rsk22h12ks2vscxn0: a document which itself contains tony operators must
// be storable, and !raw is how it is said.
func TestRawPatchStoresOperatorsAsData(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		patch string
		res   string
	}{{
		name:  "match op as data",
		doc:   `{}`,
		patch: `rule: !raw {id: !glob "hot-*"}`,
		res:   `rule: {id: !glob "hot-*"}`,
	}, {
		name:  "patch op as data does not execute",
		doc:   `rule: {keep: 1}`,
		patch: `rule: !raw {tmp: !delete null, keep: 1}`,
		res:   `rule: {tmp: !delete null, keep: 1}`,
	}, {
		name:  "whole rule, operators at depth",
		doc:   `{}`,
		patch: `rule: !raw {id: !glob "hotfix-*", patch: {tmp: !delete null}}`,
		res:   `rule: {id: !glob "hotfix-*", patch: {tmp: !delete null}}`,
	}, {
		name:  "raw replaces the doc value",
		doc:   `rule: {id: x}`,
		patch: `rule: !raw {id: !not "1"}`,
		res:   `rule: {id: !not "1"}`,
	}, {
		name:  "raw over a scalar keeps its tag",
		doc:   `{}`,
		patch: `id: !raw.glob "hot-*"`,
		res:   `id: !glob "hot-*"`,
	}, {
		name:  "raw over a nested raw is data too",
		doc:   `{}`,
		patch: `rule: !raw {inner: !raw {id: !glob "x"}}`,
		res:   `rule: {inner: !raw {id: !glob "x"}}`,
	}, {
		name:  "unregistered tags compose ahead of raw",
		doc:   `{}`,
		patch: `rule: !mytag.raw {id: !glob "x"}`,
		res:   `rule: !mytag {id: !glob "x"}`,
	}, {
		name:  "raw of a plain value is the value",
		doc:   `a: 1`,
		patch: `a: !raw 2`,
		res:   `a: 2`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := mustParse(t, test.doc)
			patch := mustParse(t, test.patch)
			got, err := Patch(doc, patch)
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			want := mustParse(t, test.res)
			if !mergeop.RawEqual(got, want) {
				t.Errorf("got\n%s\nwant\n%s", encode.MustString(got), encode.MustString(want))
			}
		})
	}
}

// TestRawPatchIsNotExecution: !raw executes nothing, so RejectUnsafe has no
// quarrel with it even when the data it stores names an unsafe op.
func TestRawPatchIsNotExecution(t *testing.T) {
	doc := mustParse(t, `{}`)
	patch := mustParse(t, `rule: !raw {cmd: !pipe "sh -c 'echo pwned'"}`)
	got, err := Patch(doc, patch, mergeop.RejectUnsafe(true))
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	want := mustParse(t, `rule: {cmd: !pipe "sh -c 'echo pwned'"}`)
	if !mergeop.RawEqual(got, want) {
		t.Errorf("got\n%s\nwant\n%s", encode.MustString(got), encode.MustString(want))
	}
}

func TestRawMatchComparesLiterally(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		match string
		res   bool
	}{{
		name:  "operator tag compared, not evaluated",
		doc:   `rule: {id: !glob "hot-*"}`,
		match: `rule: !raw {id: !glob "hot-*"}`,
		res:   true,
	}, {
		name:  "same value, tag absent in doc",
		doc:   `rule: {id: "hot-*"}`,
		match: `rule: !raw {id: !glob "hot-*"}`,
		res:   false,
	}, {
		name:  "glob is not applied under raw",
		doc:   `rule: {id: "hotfix-1"}`,
		match: `rule: !raw {id: !glob "hot*"}`,
		res:   false,
	}, {
		name:  "raw is exact: extra doc fields do not match",
		doc:   `rule: {id: !glob "hot-*", stage: open}`,
		match: `rule: !raw {id: !glob "hot-*"}`,
		res:   false,
	}, {
		name:  "raw composes under an ordinary partial match",
		doc:   `rule: {id: !glob "hot-*", stage: open}`,
		match: `rule: {id: !raw.glob "hot-*"}`,
		res:   true,
	}, {
		name:  "null under raw is literal null",
		doc:   `rule: {tmp: !delete null}`,
		match: `rule: !raw {tmp: !delete null}`,
		res:   true,
	}, {
		name:  "null under raw does not wildcard",
		doc:   `rule: {tmp: !delete 1}`,
		match: `rule: !raw {tmp: !delete null}`,
		res:   false,
	}, {
		name:  "bracket style is not data",
		doc:   "rule:\n  id: !glob \"hot-*\"",
		match: `rule: !raw {id: !glob "hot-*"}`,
		res:   true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := mustParse(t, test.doc)
			m := mustParse(t, test.match)
			got, err := Match(doc, m)
			if err != nil {
				t.Fatalf("match: %v", err)
			}
			if got != test.res {
				t.Errorf("match %q against %q: got %t want %t", test.doc, test.match, got, test.res)
			}
		})
	}
}

// TestRawPatchMergesAsData: escaping is not replacing (mgg9nvt6h12krn6dksn0). A raw
// subtree merges as a patch of the same shape would, with its operator tags carried as
// data instead of dispatched, and the document keeps what the raw value does not
// mention. "Exactly this, as data" is !insert.raw, the chain a diff emits.
func TestRawPatchMergesAsData(t *testing.T) {
	tests := []struct{ name, doc, patch, res string }{{
		name:  "the control: an ordinary container patch merges",
		doc:   `c: {b: 1}`,
		patch: `c: {a: 2}`,
		res:   `c: {a: 2, b: 1}`,
	}, {
		name:  "a raw container merges too, its operator tag as data",
		doc:   `c: {b: 1}`,
		patch: `c: !raw {a: !nullify null}`,
		res:   `c: {a: !nullify null, b: 1}`,
	}, {
		name:  "a raw container of plain data merges",
		doc:   `c: {b: 1}`,
		patch: `c: !raw {a: 2}`,
		res:   `c: {a: 2, b: 1}`,
	}, {
		name:  "an operation-shaped field under raw replaces the field, not the object",
		doc:   `c: {a: 1, b: 1}`,
		patch: `c: !raw {a: !delete null}`,
		res:   `c: {a: !delete null, b: 1}`,
	}, {
		name:  "raw merges at depth",
		doc:   `c: {d: {x: 1}, b: 1}`,
		patch: `c: !raw {d: {y: !glob "*"}}`,
		res:   `c: {d: {x: 1, y: !glob "*"}, b: 1}`,
	}, {
		name:  "a raw scalar replaces the value, tag as data",
		doc:   `c: {b: 1}`,
		patch: `c: !raw.glob "*"`,
		res:   `c: !glob "*"`,
	}, {
		name:  "a raw array is positional like any array",
		doc:   `c: [7, 8, 9]`,
		patch: `c: !raw [!glob "*"]`,
		res:   `c: [!glob "*"]`,
	}, {
		name:  "!insert.raw is the replacing form",
		doc:   `c: {b: 1}`,
		patch: `c: !insert.raw {a: !nullify null}`,
		res:   `c: {a: !nullify null}`,
	}, {
		name:  "!insert alone replaces, and its child is a patch",
		doc:   `c: {b: 1}`,
		patch: `c: !insert {a: 2}`,
		res:   `c: {a: 2}`,
	}, {
		name:  "under a head comment on the document, the same merge",
		doc:   "c:\n  # note\n  {b: 1}",
		patch: `c: !raw {a: !nullify null}`,
		res:   `c: {a: !nullify null, b: 1}`,
	}, {
		name:  "under a head comment on the patch, the same merge",
		doc:   `c: {b: 1}`,
		patch: "c:\n  # note\n  !raw {a: !nullify null}",
		res:   `c: {a: !nullify null, b: 1}`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc := mustParse(t, test.doc)
			patch := mustParse(t, test.patch)
			got, err := Patch(doc, patch)
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			want := mustParse(t, test.res)
			if !mergeop.RawEqual(got, want) {
				t.Errorf("got\n%s\nwant\n%s", encode.MustString(got), encode.MustString(want))
			}
		})
	}
}
