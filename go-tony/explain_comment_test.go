package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// Explaining a match on a value's line comment names the value: a comment node has
// no place of its own, and asking it for one panicked ("parent but not in
// container") where a plain match was fine (p478tacqh12krg32msn0 item 5).
func TestExplainingAMatchOnALineCommentDoesNotPanic(t *testing.T) {
	doc, err := parse.Parse([]byte("a: 1 # note\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	lines := doc.Values[0].Comment.Lines
	pat := parseT(t, `{a: !ir {comment: !ir {lines: [`+quoteAll(lines)+`]}}}`)
	var why Explanation
	matched, err := Match(doc, pat, Explaining(&why))
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if !matched {
		t.Errorf("the comment %q did not match itself: %+v", lines, why)
	}
	// And a mismatch names the value the comment annotates.
	pat = parseT(t, `{a: !ir {comment: !ir {lines: ["# other"]}}}`)
	matched, err = Match(doc, pat, Explaining(&why))
	if err != nil || matched {
		t.Fatalf("mismatch: matched=%v err=%v", matched, err)
	}
}

func quoteAll(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += `"` + s + `"`
	}
	return out
}
