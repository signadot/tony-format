package kpath

import (
	"strings"
	"testing"
)

// A field is reached by `.`, and that held everywhere except after an element or
// key segment, where the dot was optional: `x[0]y` parsed, and parsed as `x[0].y`.
// Two spellings named one path, and only one of them was ever written -- the
// renderer emits the dot -- so the other was a spelling only the parser knew.
//
// The leniency did not stop at the missing dot. Nothing separated the closing
// bracket from what followed, so an unbalanced closer had nowhere to be refused
// and became the first character of a field name instead: `x[0]]y` read as the
// field `]y`, and `x[0]*` became a wildcard without the dot that spells one.
func TestAFieldAfterAnElementNeedsItsDot(t *testing.T) {
	refused := []string{
		"x[0]y", "x{0}y", "x(k)y", "[0]y", "{0}y", "(k)y",
		"x[0]y.z", "x[0].y[1]z",
		"x[0]'a b'", "x(k)'a b'", // a quoted field still needs the dot
		"x[0]]y", "x[0]}", "x[0])", // an unbalanced closer, which used to be a name
		"x[0]*", "x[0]*y", // a wildcard is spelled `.*`
	}
	for _, s := range refused {
		if kp, err := Parse(s); err == nil {
			t.Errorf("%q parses, as %q: the separator is optional there and nowhere else", s, kp.String())
		} else if !strings.Contains(err.Error(), "after a segment") {
			t.Errorf("%q is refused as %q, which does not say what was expected", s, err)
		}
	}

	// What may follow a closing bracket: another segment, or nothing.
	accepted := map[string]string{
		"x[0]":        "x[0]",
		"x[0].y":      "x[0].y",
		"x[0].*":      "x[0].*",
		"x[0]..y":     "x[0]..y",
		"x[0][1]":     "x[0][1]",
		"x[0]{1}":     "x[0]{1}",
		"x[0](k)":     "x[0](k)",
		"x{0}.y":      "x{0}.y",
		"x(k).y":      "x(k).y",
		"[0].y":       "[0].y",
		"x[*].y":      "x[*].y",
		"x[0].y[1].z": "x[0].y[1].z",
	}
	for s, want := range accepted {
		kp, err := Parse(s)
		if err != nil {
			t.Errorf("%q: %s", s, err)
			continue
		}
		if got := kp.String(); got != want {
			t.Errorf("%q parses as %q, want %q", s, got, want)
		}
	}
}

// `*` is the field wildcard, and it was one only before `.`, `[` and `{`. Before
// anything else it was a field NAME -- so `*[0]` meant every field and `*(k)` meant
// the single field called `*`, one character apart, with nothing to read the
// difference off. A name beginning with `*` is quoted when written, so the bare
// spelling was never one the renderer produced.
func TestALeadingStarIsAlwaysTheWildcard(t *testing.T) {
	wild := map[string]string{
		"*":     "*",
		"*.y":   "*.y",
		"*[0]":  "*[0]",
		"*{0}":  "*{0}",
		"*(k)":  "*(k)", // read as the field named `*` until this was fixed
		"*..y":  "*..y",
		"*.*":   "*.*",
		"x.*.y": "x.*.y",
	}
	for s, want := range wild {
		kp, err := Parse(s)
		if err != nil {
			t.Errorf("%q: %s", s, err)
			continue
		}
		if got := kp.String(); got != want {
			t.Errorf("%q parses as %q, want %q", s, got, want)
		}
		if strings.HasPrefix(s, "*") && !kp.FieldAll {
			t.Errorf("%q does not lead with the field wildcard", s)
		}
	}

	// A field whose name begins with `*` is spelled quoted, and only quoted.
	for _, name := range []string{"*", "**", "*a", "*.*", "*[0]"} {
		seg := Field(name).String()
		if seg == name {
			t.Errorf("the field %q renders bare as %q, where it would read as a wildcard", name, seg)
		}
		kp, err := Parse(seg)
		if err != nil {
			t.Fatalf("the field %q renders as %q, which is not a path: %s", name, seg, err)
		}
		if kp.FieldAll || kp.Field == nil || *kp.Field != name {
			t.Errorf("the field %q renders as %q, which reads back as %q", name, seg, kp.String())
		}
	}
	for _, s := range []string{"*a", "**", "*'x'"} {
		if kp, err := Parse(s); err == nil {
			t.Errorf("%q parses, as %q: a name starting with `*` takes quotes", s, kp.String())
		}
	}
}

// The half that makes the rule above a rule rather than a preference: what the
// renderer writes is what the parser takes. A path built segment by segment --
// which is how a store builds one -- goes through String, so tightening the
// parser must not refuse anything String can emit.
func TestEveryRenderedPathParsesBack(t *testing.T) {
	names := []string{"plain", "has.dot", "has[bracket]", "has]close", "has}close",
		"has)close", "has(paren)", "has space", "*", "*a", "0", ""}
	for _, name := range names {
		for _, parent := range []struct {
			path string
			segs int
		}{
			{"x[0]", 3}, {"x{0}", 3}, {"x(k)", 3}, {"x", 2}, {"", 1},
		} {
			p := ChildField(parent.path, name)
			kp, err := Parse(p)
			if err != nil {
				t.Errorf("built %q, which does not parse: %s", p, err)
				continue
			}
			if segs := SplitAll(p); len(segs) != parent.segs {
				t.Errorf("built %q, which is %d segments: %q", p, len(segs), segs)
				continue
			}
			last := kp
			for last.Next != nil {
				last = last.Next
			}
			if last.Field == nil || *last.Field != name {
				t.Errorf("built %q, whose last segment is not the field %q", p, name)
			}
		}
	}
}
