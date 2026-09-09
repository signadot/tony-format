package patches

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// An object-shaped write beneath an array the BASE holds replaces the array, which is
// what tony.Patch answers for the same pair (0v2ws9w4h12kr7stm5n0). The fold used to reach
// the array's close with the field still pending and refuse to graft it, after the
// array's elements had already streamed; the reference and the fold are held to agree.
func TestAnObjectWriteBeneathAnArrayInTheBaseReplacesIt(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		patches    []string
		want       string
	}{
		{"a field beneath the array", `{k2: [{n: 1}], other: 1}`, []string{`{k2: {n: 5}}`}, `{k2: {n: 5}, other: 1}`},
		{"a delete beneath it leaves an empty object", `{k2: [{n: 1}], other: 1}`, []string{`{k2: {n: !delete null}}`}, `{k2: {}, other: 1}`},
		{"nested containers in the array are dropped, the next key stands", `{a: {k2: [{n: [1, 2]}, {m: {z: 1}}], z: 9}, b: 2}`, []string{`{a: {k2: {x: 1}}}`}, `{a: {k2: {x: 1}, z: 9}, b: 2}`},
		{"the array is the root", `[1, 2]`, []string{`{n: 1}`}, `{n: 1}`},
		{"two writes beneath it fold together", `{k2: [1]}`, []string{`{k2: {a: 1}}`, `{k2: {b: 2}}`}, `{k2: {a: 1, b: 2}}`},
		{"an array of arrays", `{k2: [[1], [2]]}`, []string{`{k2: {n: 3}}`}, `{k2: {n: 3}}`},
		{"the control: a scalar beneath a write is replaced as before", `{k2: 7}`, []string{`{k2: {n: 5}}`}, `{k2: {n: 5}}`},
		{"the control: a write AT the array is applied there, not here", `{k2: [1, 2], o: 1}`, []string{`{k2: [3]}`}, `{k2: [3], o: 1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := mustParseSrc(t, tc.base)
			var patches []*irNode
			for _, p := range tc.patches {
				patches = append(patches, mustParseSrc(t, p))
			}
			want := mustParseSrc(t, tc.want)
			ref, err := applyPatchesToNode(base.Clone(), patches)
			if err != nil {
				t.Fatalf("reference: %v", err)
			}
			if !mergeop.RawEqual(ref, want) {
				t.Fatalf("the table disagrees with tony.Patch itself:\n reference %s\n table     %s", encode.MustString(ref), encode.MustString(want))
			}
			got, err := applyStreamingProcessor(base, patches)
			if err != nil {
				t.Fatalf("fold: %v", err)
			}
			if !mergeop.RawEqual(got, want) {
				t.Errorf("fold\n got  %s\n want %s", encode.MustString(got), encode.MustString(want))
			}
		})
	}
}

type irNode = ir.Node

func mustParseSrc(t *testing.T, src string) *irNode {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}
