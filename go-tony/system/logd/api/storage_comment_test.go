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
