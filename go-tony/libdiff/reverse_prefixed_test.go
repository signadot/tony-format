package libdiff

import (
	"testing"

	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// Reverse dispatches on the OPERATION in a tag chain, not on the chain's head.
//
// A tag composes, and what comes before the operation is the value's own labels --
// presentation among them. A patch WRITTEN in flow style carries one: `!replace {from: 1,
// to: 5}` parses with the tag `!bracket.replace`, where the same patch computed by Diff
// carries a bare `!replace`. Reading only the head found the operation in the second and
// not in the first, so Reverse returned the patch UNCHANGED, with no error -- and `o
// patch -r` then applied the patch it was asked to invert.
//
// Silent is the part that matters. The command failed here only because the !replace
// checked its from:; an !insert or a !delete would have applied backwards and said
// nothing.
func TestReverseFindsTheOpBehindPresentation(t *testing.T) {
	for _, tc := range []struct{ name, patch, want string }{
		{"replace, flow", `{a: !replace {from: 1, to: 5}}`, `{ a: !replace { from: 5 to: 1 } }`},
		{"insert, flow", `{ a: !insert { k: 1 } }`, `{ a: !delete { k: 1 } }`},
		{"delete, flow", `{ a: !delete { k: 1 } }`, `{ a: !insert { k: 1 } }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.patch))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rev, err := Reverse(n)
			if err != nil {
				t.Fatalf("Reverse: %v", err)
			}
			got := strings.Join(strings.Fields(encode.MustString(rev)), " ")
			if got != tc.want {
				t.Errorf("Reverse(%s)\n got %s\nwant %s", tc.patch, got, tc.want)
			}
		})
	}
}
