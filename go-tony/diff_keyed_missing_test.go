package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A list tagged !key(name) whose element has no name has no identity to diff by, and
// the diff falls back to position rather than crashing. It dereferenced the absent key:
// `o diff` on such a list panicked (pndzjv1xh12ksz5xmdn0).
func TestDiffOfAKeyedListWithAnElementMissingItsKey(t *testing.T) {
	for _, pair := range [][2]string{
		{`l: !key(name) [{name: a, v: 1}, {v: 2}]`, `l: !key(name) [{name: a, v: 1}, {v: 3}]`},
		{`l: !key(name) [{v: 2}]`, `l: !key(name) [{name: a, v: 1}]`},
		{`l: !key(name) [{name: a, v: 1}]`, `l: !key(name) [{v: 2}, {name: a, v: 1}]`},
	} {
		t.Run(pair[0]+" -> "+pair[1], func(t *testing.T) {
			from, err := parse.Parse([]byte(pair[0]))
			if err != nil {
				t.Fatal(err)
			}
			to, err := parse.Parse([]byte(pair[1]))
			if err != nil {
				t.Fatal(err)
			}
			d := Diff(from, to)
			got, err := Patch(from, d)
			if err != nil {
				t.Fatalf("patch with %s: %v", encode.MustString(d), err)
			}
			if left := Diff(got, to); left != nil {
				t.Errorf("Patch(from, Diff(from, to)) = %s, want %s\n diff %s",
					encode.MustString(got), encode.MustString(to), encode.MustString(d))
			}
		})
	}
}
