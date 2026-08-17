package kpath

import (
	"fmt"
	"testing"
)

// Everything which renders a path rests on this: a field NAME, written as a path
// segment, has to read back as that same name.  Where it does not, the name stops
// being a name -- `has.dot` became two segments and logd wrote the value at an
// address nobody asked for (r05ms7nch12ksxttgdn0), `has(paren)` became a key
// selector and the write failed outright, and `*` selected every field there is.
func TestFieldNameSurvivesBeingAPathSegment(t *testing.T) {
	names := []string{
		"plain", "has.dot", "a.b.c", "has/slash", "has space", "has{brace}",
		"has[bracket]", `has"quote`, `has\backslash`, "has,comma", "has(paren)",
		"has)close", "has:colon", "has#hash", "has\ttab", "has\nnewline", "",
		"0", "123", "-1", "1.5", "true", "null", "üñïçø∂é", "emoji🙂",
		"has'apostrophe", "*", "**", "***", "*a", "a*", "a*b", "*.*", "..", ".", "a..b", "trailing.",
		"- dash", "!tag", "|pipe", "@at", "$dollar", "%percent",
	}
	// and every punctuation byte, alone and inside a name, since the rule is about
	// what a path reads as structure and that is a property of the character
	for c := byte(0x21); c < 0x7f; c++ {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		names = append(names, string(c), "a"+string(c)+"b")
	}

	for _, name := range names {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			seg := Field(name).String()
			if _, err := Parse(seg); err != nil {
				t.Fatalf("rendered as %q, which is not a path: %s", seg, err)
			}
			segs := SplitAll(seg)
			if len(segs) != 1 {
				t.Fatalf("rendered as %q, which is %d segments: %q", seg, len(segs), segs)
			}
			got, isField := SegmentFieldName(segs[0])
			if !isField {
				t.Fatalf("rendered as %q, which is not read back as a field", seg)
			}
			if got != name {
				t.Fatalf("rendered as %q, which reads back as %q", seg, got)
			}

			// and as a child of something, which is what a store builds
			p := ChildField("demo.probe", name)
			if _, err := Parse(p); err != nil {
				t.Fatalf("child path %q is not a path: %s", p, err)
			}
			if segs := SplitAll(p); len(segs) != 3 {
				t.Fatalf("child path %q is %d segments: %q", p, len(segs), segs)
			}
		})
	}
}
