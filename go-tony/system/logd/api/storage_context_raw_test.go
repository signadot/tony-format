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
		{
			// Escaping a LEAF has nowhere to put the label but the node's own tag, so
			// !irtype escaped is !raw.irtype. This is the form an escaping writer
			// actually produces -- verse's entity.AsData rewrites each tagged node's
			// tag as "!raw." + what it had -- and the walk read straight past the
			// escape into the data it was escaping (fch8ptynh12ksfvvjdn0).
			name: "escaped onto the node's own tag, which is what escaping a leaf gives",
			doc:  `{says: !raw.irtype null}`,
		},
		{
			// Where the escape sits in the chain is what decides, because a chain is
			// read left to right: an operation BEFORE the escape binds and is applied
			// to the raw data, while everything after it is the data. The row above and
			// this one are the same two labels in the two orders.
			name:    "an operation before the escape still binds",
			doc:     `{says: !strdiff.raw null}`,
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

// NeedsLowering asks the same question of the same chain -- "is there an operation here a
// store may not keep" -- and lowering.go says it must: an operation it misses is one stored
// unlowered. So it has to agree with ValidateForStorage about the escape, and it did not:
// it answered that a correctly escaped write needed lowering, and ValidateForStorage then
// refused the delta that produced (fch8ptynh12ksfvvjdn0).
func TestNeedsLoweringStopsAtRawToo(t *testing.T) {
	for _, tc := range []struct {
		doc    string
		lowers bool
	}{
		{`{says: !raw.irtype null}`, false},
		{`{says: !raw {inner: !irtype null}}`, false},
		{`{says: !irtype null}`, true},
		{`{says: !strdiff.raw null}`, true},
		{`{says: !insert.raw null}`, false},
		{`{says: !insert.strdiff null}`, true},
	} {
		t.Run(tc.doc, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.doc))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			op, got := NeedsLowering(n)
			if got != tc.lowers {
				t.Errorf("NeedsLowering = (%q, %v), want %v", op, got, tc.lowers)
			}
			// The two answers are one question. A write that needs no lowering must be
			// storable as it stands, and a write that is refused must be one lowering
			// was asked to rescue.
			if storable := ValidateForStorage(n) == nil; storable == tc.lowers {
				t.Errorf("NeedsLowering=%v but storable=%v: the two walks disagree", tc.lowers, storable)
			}
		})
	}
}
