package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// !rename did not rename. It copied the value to the new name and left the old
// field where it was, so the example in the operator's own documentation
//
//	!rename
//	- from: spec
//	  to: sp
//
// answered with both spec and sp. Where the new name was already taken it kept
// the old field and overwrote the new one, which is the same mistake reading as
// data loss instead of duplication.
//
// It went through ir.ToMap and ir.FromMap, which cost more than the move: ToMap
// skips a null-typed key and FromMap cannot put one back, so every merge key in
// the object was deleted, and a non-string key collapsed onto "".
//
// The op is byte-identical to its first commit and has never had a test. Nothing
// in the tree calls it and Diff never emits it -- it is reachable by hand,
// through `o patch`, which is how it stayed this way.
func TestRename(t *testing.T) {
	tests := []struct {
		name      string
		doc, tony string
		want      string
		// err is a fragment of the refusal, for the cases that have one.
		err string
	}{{
		// The documented example.
		name: "the field is moved, not copied",
		doc: `
spec:
  a: 1
other: 2`,
		tony: `
!rename
- from: spec
  to: sp`,
		want: `
sp:
  a: 1
other: 2`,
	}, {
		name: "a from that names no field renames nothing",
		doc:  `{a: 1}`,
		tony: "!rename\n- from: nope\n  to: x",
		want: `{
  a: 1
}`,
	}, {
		// Simultaneous: each field is read under the name it arrived with.
		name: "two fields exchange their names",
		doc: `
a: 1
b: 2`,
		tony: `
!rename
- from: a
  to: b
- from: b
  to: a`,
		want: `
b: 1
a: 2`,
	}, {
		// The same list written the other way round says the same thing.
		name: "the order the pairs are written does not matter",
		doc: `
a: 1
b: 2`,
		tony: `
!rename
- from: b
  to: a
- from: a
  to: b`,
		want: `
b: 1
a: 2`,
	}, {
		name: "a chain moves each field once",
		doc: `
a: 1
b: 2`,
		tony: `
!rename
- from: a
  to: b
- from: b
  to: c`,
		want: `
b: 1
c: 2`,
	}, {
		// A merge key names no field, so no renaming names it. Going through a
		// map deleted it: the field is a null-typed key, which ir.ToMap skips.
		//
		// It comes back written `"<<"` rather than `<<`, which is not this
		// operation's doing -- the same document parsed and re-encoded with
		// nothing applied to it renders the same way, and a merge key is what
		// the IR still holds. That is the encoder's to answer for
		// (nfs2rkf3h12kr5gth1n0), and fixing it flips this expectation on
		// purpose; what is being checked here is that the key is STILL THERE.
		name: "a merge key is left where it is",
		doc: `
a: 1
<<: "{{ tpl }}"
b: 2`,
		tony: `
!rename
- from: a
  to: z`,
		want: `
z: 1
"<<": "{{ tpl }}"
b: 2`,
	}, {
		name: "the fields keep the order they had",
		doc: `
z: 1
a: 2`,
		tony: `
!rename
- from: z
  to: y`,
		want: `
y: 1
a: 2`,
	}, {
		// Renaming onto a field which is still there would lose one of the two.
		name: "a collision with a field that stays",
		doc: `
a: 1
b: 2`,
		tony: `
!rename
- from: a
  to: b`,
		err: `would both be "b"`,
	}, {
		name: "one field named twice",
		doc:  `{a: 1}`,
		tony: "!rename\n- from: a\n  to: b\n- from: a\n  to: c",
		err:  `renamed twice`,
	}, {
		name: "an object is what it applies to",
		doc:  `[1, 2]`,
		tony: "!rename\n- from: a\n  to: b",
		err:  "cannot rename fields in non-object",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc))
			if err != nil {
				t.Fatalf("doc: %s", err)
			}
			patch, err := parse.Parse([]byte(test.tony))
			if err != nil {
				t.Fatalf("patch: %s", err)
			}
			got, err := Patch(doc, patch)
			if test.err != "" {
				if err == nil {
					t.Fatalf("it answered %s, and should have refused",
						encode.MustString(got))
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
			if s := strings.TrimSpace(encode.MustString(got)); s != strings.TrimSpace(test.want) {
				t.Errorf("got\n%s\nwant\n%s", s, strings.TrimSpace(test.want))
			}
		})
	}
}
