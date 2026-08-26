package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// !let bound names for a match and refused to patch at all -- "cannot patch with
// let operation" -- so the one place a name earns the most, writing the same
// value into more than one spot, was the one place it could not be written. The
// body had to be spelled out with the value copied into each, which is what a
// binding is for (nxybjwvch12ksbj8hxn0).
//
// A patch is the same act as a match here: the names are substituted through the
// body, and what comes out is applied. Everything the match learned about scope
// holds, because it is the same expansion -- these cases say so rather than
// assuming it.
func TestLetPatches(t *testing.T) {
	for _, tc := range []struct {
		name, doc, patch, want string
	}{{
		// the shape the issue is about
		name:  "one name, written into two places",
		doc:   `{a: {image: old}, b: {image: old}}`,
		patch: `!let {let: [{i: new}], in: {a: {image: .[i]}, b: {image: .[i]}}}`,
		want:  `{a: {image: new}, b: {image: new}}`,
	}, {
		name:  "the body is the reference itself",
		doc:   `{}`,
		patch: `!let {let: [{v: {x: 1}}], in: .[v]}`,
		want:  `{x: 1}`,
	}, {
		name:  "a name inside another operation's operand",
		doc:   `{spec: {replicas: 1}}`,
		patch: `!let {let: [{n: 3}], in: !if {if: {spec: {replicas: 1}}, then: {spec: {replicas: .[n]}}}}`,
		want:  `{spec: {replicas: 3}}`,
	}, {
		// The let is not obliged to be the whole patch, and the field it does not
		// name is left alone. The order is the patch engine's -- a field the patch
		// does not mention keeps its place ahead of one it does -- not the let's.
		name:  "a let below the root",
		doc:   `{spec: {replicas: 1}, other: 2}`,
		patch: `{spec: !let {let: [{n: 5}], in: {replicas: .[n]}}}`,
		want:  `{other: 2, spec: {replicas: 5}}`,
	}, {
		// the value bound is a node, and a node may be an operation
		name:  "a name bound to an operation",
		doc:   `{a: 1, b: 2}`,
		patch: `!let {let: [{gone: !delete null}], in: {a: .[gone]}}`,
		want:  `{b: 2}`,
	}, {
		name:  "composed with another op",
		doc:   `[{v: 1}, {v: 2}]`,
		patch: `!all.let {let: [{n: 8}], in: {v: .[n]}}`,
		want:  `[{v: 8}, {v: 8}]`,
	}, {
		name:  "an inner binding shadows an outer one",
		doc:   `{a: 1}`,
		patch: `!let {let: [{n: 2}], in: !let {let: [{n: 9}], in: {a: .[n]}}}`,
		want:  `{a: 9}`,
	}, {
		name:  "an inner binding is read in the outer scope",
		doc:   `{a: 1}`,
		patch: `!let {let: [{o: 7}], in: !let {let: [{n: .[o]}], in: {a: .[n]}}}`,
		want:  `{a: 7}`,
	}, {
		name:  "an inner body may use an outer binding",
		doc:   `{a: 1}`,
		patch: `!let {let: [{o: 7}], in: !let {let: [{n: 9}], in: {a: .[o]}}}`,
		want:  `{a: 7}`,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Patch(mustParseNode(t, tc.doc), mustParseNode(t, tc.patch))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			if got == nil {
				t.Fatalf("patch answered nothing, want %s", tc.want)
			}
			out, want := encode.MustString(got), encode.MustString(mustParseNode(t, tc.want))
			if out != want {
				t.Errorf("got:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

// A name no let binds is refused rather than expanded, and refusing matters more
// on the patch side than the match side: an unbound .[name] expands to NULL, and
// where a null pattern merely matched everything, a null PATCH is written -- the
// field the body meant to set is nulled out, and the document is wrong rather
// than the answer.
func TestLetPatchRefusesWhatItCannotBind(t *testing.T) {
	for _, tc := range []struct {
		name, patch, want string
	}{{
		name:  "a name the let does not bind",
		patch: `!let {let: [{n: 2}], in: {a: .[nope]}}`,
		want:  "does not bind .[nope]",
	}, {
		name:  "a name no let binds, from inside a nested one",
		patch: `!let {let: [{a: 1}], in: !let {let: [{b: 2}], in: {a: .[nope]}}}`,
		want:  "does not bind .[nope]",
	}, {
		name:  "a binding that binds nothing",
		patch: `!let {let: [{}], in: {a: 2}}`,
		want:  "binds nothing",
	}, {
		name:  "a binding which is not an object",
		patch: `!let {let: [3], in: {a: 2}}`,
		want:  "a binding is an object",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			doc := mustParseNode(t, `{a: 1}`)
			got, err := tony.Patch(doc, mustParseNode(t, tc.patch))
			if err == nil {
				t.Fatalf("no error, result %s; want an error saying %q", encode.MustString(got), tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
		})
	}
}

// The registry is what the operator tables in the docs are checked against, and
// what a reader is told they may write.
func TestLetIsRegisteredAsBoth(t *testing.T) {
	sym := mergeop.Let()
	if !sym.IsMatch() || !sym.IsPatch() {
		t.Errorf("!let: match=%v patch=%v, want both", sym.IsMatch(), sym.IsPatch())
	}
}
