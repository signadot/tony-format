package api

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// TestValidateForStorage_RawIsData: !raw is the one escape that lets a document
// holding operators be stored as data -- a charter, a stored rule, a stored
// patch -- and the storage vocabulary has to stop at it.
//
// Walking into it refused a write whose author escaped it exactly as the format
// says to, and refused it at the storage boundary where nothing can be done
// about it (issue 6225etzfh12kr955fxn0).
func TestValidateForStorage_RawIsData(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		wantErr   string // empty: must be storable
	}{
		{
			name: "a bare unstorable operation is still refused",
			doc:  `{rules: {value: !let {let: [{tip: abc}], in: {state: open}}}}`,
			// let is conditional on the document it meets
			wantErr: `operation "!let"`,
		},
		{
			name: "the same operation under !raw is data",
			doc:  `{rules: !raw {value: !let {let: [{tip: abc}], in: {state: open}}}}`,
		},
		{
			name: "under !insert.raw, which is what a diff emits for it",
			doc:  `{rules: !insert.raw {value: !let {let: [{tip: abc}], in: {state: open}}}}`,
		},
		{
			name: "other operators as data: glob, irtype, not, strdiff",
			doc:  `{rules: !raw {a: !glob "verse/auto/*", b: !irtype "", c: !not zzz, d: !strdiff [x]}}`,
		},
		{
			name: "a nested raw deep in a document",
			doc:  `{a: {b: {c: !raw {d: !if {if: {x: 1}, then: !pass null}}}}}`,
		},
		{
			name:    "the node's own chain is still checked: strdiff over raw",
			doc:     `{rules: !strdiff.raw [x]}`,
			wantErr: `operation "!strdiff"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.doc))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			err = ValidateForStorage(n)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("refused a document escaped with !raw: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}
