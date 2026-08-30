package api

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// A tag composes with '.', so !insert.retag(x,y) names TWO operations and the second
// binds as much as the first. Both storage checks used to ask mergeop.SplitChild,
// which answers the head only: it said "insert, storable" and stopped, so a relative
// operation written behind an absolute one passed the vocabulary entirely
// (2zq8g336h12kr118j5n0).
//
// That is the guard a scoped write goes through. 3xn08cb6h12kr4psg5n0 records what a
// !replace that gets through costs: the scope is unreadable from the next baseline
// write onward and DeleteScope is the only repair.
func TestComposedTagIsReadWhole(t *testing.T) {
	tests := []struct {
		name, doc, want string // want is the operation named, "" if storable
	}{{
		name: "the plain form was always refused",
		doc:  "{a: !retag(x,y) 5}",
		want: "retag",
	}, {
		name: "and so is it behind an absolute one",
		doc:  "{a: !insert.retag(x,y) 5}",
		want: "retag",
	}, {
		name: "a positional array diff behind an insert",
		doc:  "{a: !insert.arraydiff {}}",
		want: "arraydiff",
	}, {
		name: "a checked replace behind a delete",
		doc:  "{a: !delete.replace {from: 1, to: 2}}",
		want: "replace",
	}, {
		name: "the relative one first, which was always caught",
		doc:  "{a: !strdiff.raw []}",
		want: "strdiff",
	}, {
		name: "two absolute ones compose to something storable",
		doc:  "{a: !insert.addtag(x) 5}",
	}, {
		// A chain carries data and presentation tags too, and those are not
		// operations at all -- refusing them would refuse every bracketed or
		// octal value a client writes.
		name: "a presentation tag is not an operation",
		doc:  "{a: !insert.bracket [1, 2]}",
	}, {
		name: "nor is a data tag",
		doc:  "{a: !insert.t1 5}",
	}, {
		name: "nor a number notation",
		doc:  "{a: !insert.oct 0o644}",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(test.doc))
			if err != nil {
				t.Fatalf("parse: %s", err)
			}
			err = ValidateForStorage(n)
			op, needs := NeedsLowering(n)

			if test.want == "" {
				if err != nil {
					t.Errorf("ValidateForStorage refused a storable patch: %s", err)
				}
				if needs {
					t.Errorf("NeedsLowering named %q for a storable patch", op)
				}
				return
			}
			if err == nil {
				t.Errorf("ValidateForStorage accepted a patch carrying !%s", test.want)
			} else if !strings.Contains(err.Error(), "!"+test.want) {
				t.Errorf("refused with %q, which does not name !%s", err, test.want)
			}
			// The two have to agree: one decides whether an artefact may be
			// appended, the other whether a write must be lowered first, and a
			// patch that needs lowering is exactly one that may not be stored.
			if !needs {
				t.Errorf("NeedsLowering said no for a patch carrying !%s", test.want)
			} else if op != "!"+test.want {
				t.Errorf("NeedsLowering named %q, want %q", op, "!"+test.want)
			}
		})
	}
}
