package api

import (
	"strings"
	"testing"
)

// TestValidateSeesThroughComments: the walk switched on a node's type, and a head
// comment is a wrapper, so an operation written under one was never checked. It
// was unreachable while nothing could put a comment into a store; a store that
// keeps comments makes it reachable from any client (3cdjz00jh12krns4g1n0).
func TestValidateSeesThroughComments(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		wantErr   string
	}{
		{"an unstorable op", "a: !strdiff \"@@ -1 +1 @@\"\n", "may not be stored"},
		{"the same op under a comment", "# note\na: !strdiff \"@@ -1 +1 @@\"\n", "may not be stored"},
		{"under a comment, nested", "a:\n  # note\n  b: !strdiff \"@@ -1 +1 @@\"\n", "may not be stored"},
		{"a storable op under a comment", "# note\na: !insert 1\n", ""},
		{"a comment and no op at all", "# note\na: 1\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateForStorage(mustParseCommented(t, tc.src))
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("refused a storable write: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("accepted %q, which is not storable", tc.src)
			case tc.wantErr != "" && err != nil && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("the error does not say why: %v", err)
			}
		})
	}
}

// TestValidateReadsTheTagUnderAComment: the wrapper and what it wraps are one
// value, and the tag is the value's. Both walks read the tag before stepping
// through the comment, so they read the wrapper's, which has none: an escape
// under a comment was walked into, refusing a write escaped as the format says
// to, and an operation under one was not seen, storing it as it was written
// (vbtm1dfbh12krbkcq1n0).
func TestValidateReadsTheTagUnderAComment(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		wantErr   string // empty: storable as written, and needs no lowering
	}{
		{"an escape, for comparison", "spec: !insert.raw\n  a: !and [1, 2]\n", ""},
		{"the same escape under a comment", "spec:\n  # why\n  !insert.raw\n  a: !and [1, 2]\n", ""},
		{"escaped onto a leaf, under a comment", "spec:\n  # why\n  !raw.irtype null\n", ""},
		{"an escaped element of a list, under a comment", "rules:\n- # why\n  !insert.raw\n  a: !and [1, 2]\n", ""},
		{"an operation, for comparison", "spec: !replace\n  from: 1\n  to: 2\n", `operation "!replace"`},
		{"the same operation under a comment", "spec:\n  # why\n  !replace\n  from: 1\n  to: 2\n", `operation "!replace"`},
		{"an operation before the escape, under a comment", "spec:\n  # why\n  !strdiff.raw [x]\n", `operation "!strdiff"`},
		{"a storable operation under a comment", "spec:\n  # why\n  !insert\n  a: 1\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := mustParseCommented(t, tc.src)
			err := ValidateForStorage(n)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("refused a storable write: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("accepted %q, which is not storable", tc.src)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("the error does not say why: %v", err)
			}
			// The two walks are one question (TestNeedsLoweringStopsAtRawToo).
			if op, lowers := NeedsLowering(n); lowers != (tc.wantErr != "") {
				t.Errorf("NeedsLowering = (%q, %v), and storable is %v: the two walks disagree", op, lowers, err == nil)
			}
		})
	}
}
