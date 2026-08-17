package stream

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// A field name is not always its own path segment: `signadot/signadot#7349` renders
// quoted. KPathState checked itself by pasting bare names after dots, so it disagreed
// with its own rendering and panicked -- on paths which were correct. A snapshot
// holding one such path took the process down on every narrow read which seeked near
// it, which is how staging found it (0w79k6hqh12krgcwgdn0).
func TestKPathStateKeepsQuotedFieldNames(t *testing.T) {
	names := []string{
		"signadot/signadot#7349", // the one from staging
		"a.b",                    // a dot, which a segment reads as a descent
		"x[1]",
		"y{2}",
		"z(3)",
		"*star",
		"with space",
		`has"quote`,
		"#comment",
		"plain",
	}
	for _, name := range names {
		for _, tail := range []string{"", ".checks", ".checks.state", "[0]", "{7}"} {
			kp := kpath.ChildField("verse.github.pr", name) + tail
			t.Run(kp, func(t *testing.T) {
				st, err := KPathState(kp)
				if err != nil {
					t.Fatalf("KPathState(%q): %s", kp, err)
				}
				// The leaf array cases position one before the target on purpose, so
				// only the paths which name a position exactly are compared whole.
				if tail == "" || tail == ".checks" || tail == ".checks.state" || tail == "{7}" {
					if got := st.CurrentPath(); got != kp {
						t.Errorf("landed at %q, want %q", got, kp)
					}
				}
			})
		}
	}
}
