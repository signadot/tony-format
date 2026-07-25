package parse

import "testing"

// TestUpstreamItem6_WhitespaceOnlyIsBlank guards issue f69agjyeh12ks item 6: a
// whitespace-only body must be treated as an empty document regardless of which
// whitespace it is. Previously a body of spaces failed with a confusing
// "imbalanced document: extraneous indent" while "\n\t\n" and "" returned
// (nil, nil). They must all behave alike.
func TestUpstreamItem6_WhitespaceOnlyIsBlank(t *testing.T) {
	blanks := []string{"", "   ", "\t", "\n\t\n", "  \n  ", "\r\n", "\n"}
	for _, in := range blanks {
		node, err := Parse([]byte(in), ParseTony())
		if err != nil {
			t.Errorf("Parse(%q) errored, want blank/no-error: %v", in, err)
		}
		if node != nil {
			t.Errorf("Parse(%q) returned a node, want nil for a blank document", in)
		}
	}

	// Real content must still parse.
	node, err := Parse([]byte("a: 1"), ParseTony())
	if err != nil || node == nil {
		t.Fatalf("Parse(\"a: 1\") = (%v, %v), want a node and no error", node, err)
	}
}
