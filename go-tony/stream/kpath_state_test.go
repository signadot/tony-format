package stream

import "testing"

// A State steps into fields and dense and sparse elements, one position each. A
// wildcard names no one position, and a key or a `..` is no step a State takes:
// the wildcards panicked on the name or index they do not have, and a key or a
// descent was skipped -- by the landing check too, so the State stopped short of
// the path and said nothing (addsgv1yh12kszdxmdn0).
func TestKPathStateRefusesWhatItCannotStepInto(t *testing.T) {
	for _, kp := range []string{
		"a.*", "a[*]", "a{*}", "a(x)", "a..b", "a..",
		"a.*.b", "a[*].b", "a{*}.b", "a(x).b", "a(x)[0]",
		"*", "[*]", "{*}", "(x)", "..b",
	} {
		t.Run(kp, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("KPathState(%q) panicked: %v", kp, r)
				}
			}()
			if st, err := KPathState(kp); err == nil {
				t.Errorf("KPathState(%q) landed at %q, want an error", kp, st.CurrentPath())
			}
		})
	}
}
