package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// Absence is an answer; a failure to read is not.
//
// ir.Node.GetKPathWith answers (nil, nil) for a path the document does not have -- the
// idiom claimDelta reads -- and an error for something else entirely: a type mismatch on
// the way down, an index past the end, a path that will not parse. Those two were one
// branch, so a failure to READ a path became a claim that the path holds NOTHING, and
// the scope stored a delete of the client's data.
//
// A refused write is recoverable and a stored one is not, which is why this is an error
// and not a log line -- the same reason lowerWrite refuses a delta it cannot promise to
// re-apply.
func TestAClaimRefusesWhatItCannotRead(t *testing.T) {
	next, err := parse.Parse([]byte(`{a: 1, o: {b: 2}, xs: [1, 2]}`))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path string
		wantErr    bool
		want       string
	}{
		{"a path the document does not have", "missing", false, "missing: !delete null"},
		{"a path under one it does not have", "o.missing", false, "o: missing: !delete null"},
		{"through a scalar", "a.b", true, ""},
		{"an index past the end", "xs[9]", true, ""},
		{"a path that will not parse", "((", true, ""},
		{"a path it does have", "o.b", false, "o: b: !raw 2"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := claimDelta(next, []string{test.path})
			if test.wantErr {
				if err == nil {
					t.Fatalf("claiming %q was accepted, and answered %s",
						test.path, withComments(got))
				}
				if !strings.Contains(err.Error(), "cannot read it back") {
					t.Errorf("the refusal does not say why: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("claiming %q: %v", test.path, err)
			}
			if s := withComments(got); s != test.want {
				t.Errorf("claimed %s, want %s", s, test.want)
			}
		})
	}
}
