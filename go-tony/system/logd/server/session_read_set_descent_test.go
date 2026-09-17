package server

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// descentDoc holds every kind of container at more than one depth, so a descent meets
// each: an object, a dense array, a sparse array, and leaves, with a `c` at several
// depths and a `b` under a `b`. Its keys are in the store's order -- sorted -- which is
// the order the store holds any document in, so what it answers is what a list over the
// document answers.
const descentDoc = `{a: {b: {b: {c: 1}, c: 2}, c: 7, sp: !sparsearray {3: {c: 5}, 7: 6}, x: [{c: 3}, 4]}, c: 8}`

// TestSetMatch_Descent: a match at a path holding `..` answers the nodes at any depth,
// each once, in pre-order -- a node before what is under it, and at each level the
// store's order -- as the same query over the same document answers in process
// (th7sdhvyh12ksjtfn9n0).
func TestSetMatch_Descent(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", descentDoc)

	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		// Every c, wherever it is, in document order: the deepest under a first.
		{"..c", []string{"a.b.b.c", "a.b.c", "a.c", "a.sp{3}.c", "a.x[0].c", "c"}},
		{"a..c", []string{"a.b.b.c", "a.b.c", "a.c", "a.sp{3}.c", "a.x[0].c"}},
		// The node itself included, first.
		{"a.b..", []string{"a.b", "a.b.b", "a.b.b.c", "a.b.c"}},
		// Path after the descent: a c under a b, wherever the b is.
		{"a..b.c", []string{"a.b.b.c", "a.b.c"}},
		// Two descents reach a.b.b.c two ways: once. Two in a row are one.
		{"a..b..c", []string{"a.b.b.c", "a.b.c"}},
		{"a....c", []string{"a.b.b.c", "a.b.c", "a.c", "a.sp{3}.c", "a.x[0].c"}},
		// A wildcard after a descent is kind-strict: positions, sparse keys, fields.
		{"..[*]", []string{"a.x[0]", "a.x[1]"}},
		{"..{*}", []string{"a.sp{3}", "a.sp{7}"}},
		{"a.x..*", []string{"a.x[0].c"}},
		// A concrete position or sparse key after a descent.
		{"..[1]", []string{"a.x[1]"}},
		{"..{7}", []string{"a.sp{7}"}},
		// Nothing there: the marker alone, at any depth or at an absent prefix.
		{"..nope", nil},
		{"nope..", nil},
		{"a.c..", []string{"a.c"}},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			answers := runSet(t, store, `{id: "q", match: {path: "`+tc.pattern+`", return: path}}`)
			got := mustSet(t, answers, "q").paths()
			if !equalStrings(got, tc.want) {
				t.Errorf("%q answered %v, want %v", tc.pattern, got, tc.want)
			}
		})
	}

	// `..` alone: everything, the root first. The root's path is empty, as the single-
	// node read reports it: the member with no path.
	all := mustSet(t, runSet(t, store, `{id: "all", match: {path: "..", return: path}}`), "all")
	if n := len(all.members); n != 16 || all.members[0].Path != "" || all.members[1].Path != "a" {
		t.Errorf("`..` answered %d members starting %v, want 16 with the root first", n, all.paths()[:min(3, n)])
	}
}

// TestSetMatch_DescentAgreesWithListKPath: the same document, as a node and as a store,
// answers every descent query with the same paths in the same order. Uniformity is a
// fact to check rather than a rule to state.
func TestSetMatch_DescentAgreesWithListKPath(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", descentDoc)
	doc, err := parse.Parse([]byte(descentDoc))
	if err != nil {
		t.Fatal(err)
	}
	// Not `..{*}`: in process {*} names the positions of a dense array as well, which is
	// ir's own reading of {*} and no descent's, and is not the store's.
	for _, pattern := range []string{
		"..c", "a..c", "a.b..", "a..b.c", "a..b..c", "a....c", "..[*]", "a.x..*",
		"..[1]", "..{7}", "..nope", "..", "a.x[*]..", "a..b.*", "a.sp..",
	} {
		t.Run(pattern, func(t *testing.T) {
			found, err := doc.ListKPath(nil, pattern)
			if err != nil {
				t.Fatalf("ListKPath: %v", err)
			}
			var inProcess []string
			for _, n := range found {
				inProcess = append(inProcess, n.KPath())
			}
			onWire := mustSet(t, runSet(t, store, `{id: "q", match: {path: "`+pattern+`", return: path}}`), "q").paths()
			if strings.Join(inProcess, " ") != strings.Join(onWire, " ") {
				t.Errorf("%q: in process %v, on the wire %v", pattern, inProcess, onWire)
			}
		})
	}
}

// TestSetMatch_DescentPattern: the request's pattern selects per node, at whatever depth
// the node is.
func TestSetMatch_DescentPattern(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", `{jobs: {a: {status: done, sub: {status: done}}, b: {status: ready}}, status: done}`)
	answers := runSet(t, store,
		`{id: "done", match: {path: "..", data: {status: done}, return: path}}`,
		`{id: "bodies", match: {path: "..status", data: done}}`,
	)
	if got := mustSet(t, answers, "done").paths(); !equalStrings(got, []string{"", "jobs.a", "jobs.a.sub"}) {
		t.Errorf("the nodes with a done status: %v", got)
	}
	bodies := mustSet(t, answers, "bodies")
	if got := bodies.paths(); !equalStrings(got, []string{"jobs.a.status", "jobs.a.sub.status", "status"}) {
		t.Errorf("..status that is done: %v", got)
	}
	for _, m := range bodies.members {
		if m.Body == nil || m.Body.String != "done" {
			t.Errorf("%s: body %v", m.Path, m.Body)
		}
	}
}

// TestSetMatch_DescentKeyed: a keyed array's elements are named by (*) after a descent
// and not by .* or [*], and an element named by its key is found at any depth; a (key)
// meeting an array that is not keyed names nothing there rather than faulting the walk.
func TestSetMatch_DescentKeyed(t *testing.T) {
	store := openStore(t)
	schema, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}, deep: {runs: {id: !logd-key null}}}}`))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	narrowWrite(t, store, "", `{runs: [{id: r1, n: 1}], deep: {runs: [{id: r1, n: 2}, {id: r2, n: 3}]}, plain: [{id: r1}]}`)

	answers := runSet(t, store,
		`{id: "elements", match: {path: "..(*)", return: path}}`,
		`{id: "fields", match: {path: "..runs.*", return: path}}`,
		`{id: "positions", match: {path: "..runs[*]", return: path}}`,
		`{id: "byKey", match: {path: "..(r1)", return: path}}`,
		`{id: "byKeyUnder", match: {path: "..runs(r2).n", return: "path,body"}}`,
		`{id: "kinds", match: {path: "..runs", return: "path,iterType"}}`,
		`{id: "elementFields", match: {path: "..(*).*", return: path}}`,
		`{id: "oneElementFields", match: {path: "runs(*).*", return: path}}`,
	)
	if got := mustSet(t, answers, "elements").paths(); !equalStrings(got, []string{`deep.runs."(id=r1)"`, `deep.runs."(id=r2)"`, `runs."(id=r1)"`}) {
		t.Errorf("..(*) answered %v", got)
	}
	// An element of a keyed array is an object, whatever its path elides to in the
	// schema: .* names its fields, after a descent and after (*) alike.
	if got := mustSet(t, answers, "elementFields").paths(); !equalStrings(got, []string{
		`deep.runs."(id=r1)".id`, `deep.runs."(id=r1)".n`, `deep.runs."(id=r2)".id`, `deep.runs."(id=r2)".n`,
		`runs."(id=r1)".id`, `runs."(id=r1)".n`}) {
		t.Errorf("..(*).* answered %v", got)
	}
	if got := mustSet(t, answers, "oneElementFields").paths(); !equalStrings(got, []string{`runs."(id=r1)".id`, `runs."(id=r1)".n`}) {
		t.Errorf("runs(*).* answered %v", got)
	}
	if got := mustSet(t, answers, "fields").paths(); len(got) != 0 {
		t.Errorf("..runs.* over keyed arrays answered %v, want nothing", got)
	}
	if got := mustSet(t, answers, "positions").paths(); len(got) != 0 {
		t.Errorf("..runs[*] answered %v, want nothing: plain is not under a runs", got)
	}
	if got := mustSet(t, answers, "byKey").paths(); !equalStrings(got, []string{`deep.runs."(id=r1)"`, `runs."(id=r1)"`}) {
		t.Errorf("..(r1) answered %v", got)
	}
	under := mustSet(t, answers, "byKeyUnder")
	if got := under.paths(); !equalStrings(got, []string{`deep.runs."(id=r2)".n`}) {
		t.Errorf("..runs(r2).n answered %v", got)
	}
	var kinds []string
	for _, m := range mustSet(t, answers, "kinds").members {
		kinds = append(kinds, m.Path+":"+m.IterType)
	}
	if want := []string{"deep.runs:" + api.IterKeyedArray, "runs:" + api.IterKeyedArray}; !equalStrings(kinds, want) {
		t.Errorf("..runs kinds %v, want %v", kinds, want)
	}
}
