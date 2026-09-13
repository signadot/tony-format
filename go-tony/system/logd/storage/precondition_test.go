package storage

import (
	"fmt"
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

func mustPattern(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

// matchVia evaluates pattern at kp the way evaluateMatches does, over the node read
// answers: nil is null.
func matchVia(t *testing.T, read func() (*ir.Node, error), pattern *ir.Node) bool {
	t.Helper()
	cur, err := read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cur == nil {
		cur = ir.Null()
	}
	ok, err := tony.Match(cur, pattern)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	return ok
}

// What a precondition reads is what its pattern names, and the matcher answers the
// same for it as for the whole value: every shape of question, over every shape of
// value, in baseline and in a scope (mgn9szemh12krh7zmsn0).
func TestPreconditionNarrowingIsExact(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{
  kind: {e1: {owner: a, n: 1}, e2: {owner: b, n: 2}, "dot.ted": {owner: c}},
  nul: null,
  str: hello,
  num: 7,
  arr: [1, 2, 3],
  sparse: {0: x, 2: y},
  nested: {a: {b: {c: deep}}},
}`)
	scope := "s1"
	mustCommit(t, s, &scope, `{kind: {e3: {owner: s}, e1: {n: 11}}, str: scoped}`)
	head, _ := s.GetCurrentCommit()

	for _, tc := range []struct{ path, pattern string }{
		// shape-only
		{"kind", `!or [{}, !irtype null]`},
		{"nul", `!or [{}, !irtype null]`},
		{"str", `!or [{}, !irtype null]`},
		{"arr", `!or [{}, !irtype null]`},
		{"absent", `!or [{}, !irtype null]`},
		{"kind", `{}`},
		{"str", `{}`},
		{"arr", `!irtype []`},
		// one named child, present and absent, null and absent apart
		{"kind", `{e1: {}}`},
		{"kind", `{e3: {}}`},
		{"kind", `!not {nope: !not.or [{}, !irtype null]}`},
		{"kind", `!not {e1: !not.or [{}, !irtype null]}`},
		{"", `!not {nul: !not.or [{}, !irtype null]}`},
		{"", `{nul: !irtype null}`},
		{"", `{nul: null}`},
		{"kind", `{"dot.ted": {owner: c}}`},
		{"kind", `{"dot.ted": {owner: x}}`},
		// nested fields and scalar comparisons
		{"nested", `{a: {b: {c: deep}}}`},
		{"nested", `{a: {b: {c: shallow}}}`},
		{"nested", `{a: {b: !irtype ""}}`},
		{"", `{str: hello, num: 7}`},
		{"", `{str: hello, num: 8}`},
		{"str", `hello`},
		{"num", `7`},
		{"kind", `hello`},
		{"", `!and [{str: hello}, {kind: {e2: {n: 2}}}]`},
		{"", `!and [{str: hello}, {kind: {e2: {n: 3}}}]`},
		{"", `!or [{str: bye}, {kind: {e2: {}}}]`},
		// whole-value questions, which fall back to the value
		{"arr", `[1, 2, 3]`},
		{"arr", `[1, 2]`},
		{"sparse", `!sparsearray {0: x}`},
		{"kind", `!all {owner: !irtype ""}`},
		{"kind", `!all {n: !irtype 1}`},
		{"arr", `!all.irtype 1`},
	} {
		pattern := mustPattern(t, tc.pattern)
		for _, sc := range []*string{nil, &scope} {
			name := tc.path + " " + tc.pattern
			if sc != nil {
				name += " in " + *sc
			}
			wholeAnswer := matchVia(t, func() (*ir.Node, error) { return s.stateAt(head, sc, tc.path) }, pattern)
			narrowAnswer := matchVia(t, func() (*ir.Node, error) { return s.stateFor(head, sc, tc.path, pattern) }, pattern)
			if wholeAnswer != narrowAnswer {
				t.Errorf("%s: whole value says %v, what the pattern names says %v", name, wholeAnswer, narrowAnswer)
			}
		}
	}
}

// A precondition that asks about a parent's shape, or one child of it, costs that and not
// the parent's children.
func TestPreconditionReadsWhatItNames(t *testing.T) {
	s := openTestStorage(t)
	var b strings.Builder
	b.WriteString("{big: {kind: {")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "e%d: {owner: someone, grants: [{read: [\"*\"]}]},", i)
	}
	b.WriteString("}}}")
	mustCommit(t, s, nil, b.String())
	head, _ := s.GetCurrentCommit()

	emitted := func(read func() (*ir.Node, error)) int64 {
		before := s.ReadStats().BytesEmitted
		if _, err := read(); err != nil {
			t.Fatal(err)
		}
		return s.ReadStats().BytesEmitted - before
	}
	all := emitted(func() (*ir.Node, error) { return s.stateAt(head, nil, "big.kind") })
	for _, src := range []string{
		`!or [{}, !irtype null]`,
		`!not {nope: !not.or [{}, !irtype null]}`,
		`{e5: {}}`,
	} {
		pattern := mustPattern(t, src)
		got := emitted(func() (*ir.Node, error) { return s.stateFor(head, nil, "big.kind", pattern) })
		if got*100 > all {
			t.Errorf("%s at big.kind read %d bytes; the whole value is %d", src, got, all)
		}
		if !matchVia(t, func() (*ir.Node, error) { return s.stateFor(head, nil, "big.kind", pattern) }, pattern) {
			t.Errorf("%s at big.kind does not hold", src)
		}
	}
}

func BenchmarkPreconditionAtWideParent(b *testing.B) {
	dir := b.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	var sb strings.Builder
	sb.WriteString("{big: {kind: {")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&sb, "e%d: {owner: someone, grants: [{read: [\"*\"]}]},", i)
	}
	sb.WriteString("}}}")
	patch, _ := parse.Parse([]byte(sb.String()))
	txn, _ := s.NewTx(1, nil)
	p, _ := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
	if r := p.Commit(); !r.Committed {
		b.Fatal(r.Error)
	}
	head, _ := s.GetCurrentCommit()
	pattern, _ := parse.Parse([]byte(`!not {nope: !not.or [{}, !irtype null]}`))
	b.Run("whole value", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.stateAt(head, nil, "big.kind"); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("what the pattern names", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := s.stateFor(head, nil, "big.kind", pattern); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// The kind of a path is decided from the index over a history of many commits -- writes
// below the path not decoded, statements at it applied in order -- and handed to the
// read where the index cannot decide it. Every history here is compared against the
// whole value.
func TestPreconditionKindOverAHistory(t *testing.T) {
	s := openTestStorage(t)
	scope := "s1"
	steps := []struct {
		scope *string
		src   string
	}{
		{nil, `{kind: {e1: {n: 1}}}`},
		{nil, `{kind: {e2: {n: 2}}}`},     // a second child: still an object
		{nil, `{gone: {e1: {n: 1}}}`},     // an object,
		{nil, `{gone: !delete null}`},     // then deleted after its children were written
		{nil, `{sc: 5}`},                  // a scalar,
		{nil, `{sc: {e9: {}}}`},           // replaced by a child write
		{nil, `{nl: null}`},               // a null,
		{nil, `{nl: {e9: {}}}`},           // replaced by a child write
		{nil, `{arr: [1, 2, 3]}`},         // an array,
		{nil, `{arr: !arraydiff {0: 9}}`}, // written into by position
		{nil, `{re: {a: 1}}`},             // an object,
		{nil, `{re: 7}`},                  // replaced by a scalar
		{nil, `{wrap: {inner: {x: 1}}}`},  //
		{nil, `{wrap: !delete null}`},     // deleted above the path asked about
		{&scope, `{kind: {e3: {n: 3}}}`},  // a scope adds a child
		{&scope, `{gone: {back: 1}}`},     // and revives a deleted parent
		{&scope, `{re: {b: 2}}`},          // and turns a scalar back into an object
		{&scope, `{kind: !delete null}`},  // and deletes what baseline holds
	}
	for _, st := range steps {
		mustCommit(t, s, st.scope, st.src)
	}
	head, _ := s.GetCurrentCommit()

	shape := mustPattern(t, `!or [{}, !irtype null]`)
	for _, path := range []string{"kind", "gone", "sc", "nl", "arr", "re", "wrap", "wrap.inner", "kind.e1", "kind.e3", "absent"} {
		for _, pattern := range []*ir.Node{
			shape,
			mustPattern(t, `!irtype []`),
			mustPattern(t, `{e1: {}}`),
			mustPattern(t, `!not {e9: !not.or [{}, !irtype null]}`),
			mustPattern(t, `7`),
		} {
			for _, sc := range []*string{nil, &scope} {
				name := path + " " + pattern.Path()
				if sc != nil {
					name += " in " + *sc
				}
				wholeAnswer := matchVia(t, func() (*ir.Node, error) { return s.stateAt(head, sc, path) }, pattern)
				narrowAnswer := matchVia(t, func() (*ir.Node, error) { return s.stateFor(head, sc, path, pattern) }, pattern)
				if wholeAnswer != narrowAnswer {
					t.Errorf("%s: whole value says %v, what the pattern names says %v", name, wholeAnswer, narrowAnswer)
				}
			}
		}
	}
}
