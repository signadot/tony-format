package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
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
	for _, pattern := range []string{
		"..c", "a..c", "a.b..", "a..b.c", "a..b..c", "a....c", "..[*]", "..{*}", "..*", "a.x..*",
		"..[1]", "..{7}", "..nope", "..", "a.x[*]..", "a..b.*", "a.sp..", "a.sp.*", "a.x{*}",
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

// TestSetMatch_DescentListsBeyondTheReadBudget: a listing over a descent costs the
// tables and reads no node. Every node beneath the descent is reached by listing, so
// `return: path` and `return: iterType` are answered from what the listing found, and
// a subtree far larger than any read budget lists all the same -- which is the case the
// issue's discussion asks for, a path listing at any depth.
func TestSetMatch_DescentListsBeyondTheReadBudget(t *testing.T) {
	store := openStore(t)
	schema, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	// Each container is larger than the budget; the walk lists every record, at about
	// a millisecond a table, which is what sizes this.
	const n = 40
	pad := strings.Repeat("x", 300)
	var jobs, nums, list, runs []string
	for i := range n {
		jobs = append(jobs, fmt.Sprintf("j%03d: {note: %s, status: done}", i, pad))
		nums = append(nums, fmt.Sprintf("%d: {note: %s}", i, pad))
		list = append(list, fmt.Sprintf("{note: %s}", pad))
		runs = append(runs, fmt.Sprintf("{id: r%03d, note: %s}", i, pad))
	}
	narrowWrite(t, store, "", "{jobs: {"+strings.Join(jobs, ", ")+"}, list: ["+strings.Join(list, ", ")+
		"], nums: !sparsearray {"+strings.Join(nums, ", ")+"}, runs: ["+strings.Join(runs, ", ")+"]}")

	ls := newLiveSessionWith(t, &SessionConfig{Storage: store, Hub: NewWatchHub(), ReadBudget: 8 << 10})
	ids := []string{"container", "status", "all", "under", "notes"}
	started := time.Now()
	for _, req := range []string{
		`{id: "container", match: {path: jobs}}`,
		`{id: "status", match: {path: "..status", return: path}}`,
		`{id: "all", match: {path: "..", return: "path,iterType"}}`,
		`{id: "under", match: {path: "jobs..", return: path}}`,
		`{id: "notes", match: {path: "..note", return: "path,body"}}`,
	} {
		ls.send(req)
	}
	got := ls.until("every listing to end", func(m map[string][]*api.SessionResponse) bool {
		for _, id := range ids {
			if len(m[id]) == 0 {
				return false
			}
			last := m[id][len(m[id])-1]
			if last.Error == nil && !(last.Result != nil && last.Result.Match != nil && last.Result.Match.Done) {
				return false
			}
		}
		return true
	})
	t.Logf("four descents over %d records under an 8 KiB budget: %v", 4*n, time.Since(started))
	answers := map[string]*setAnswer{}
	for id, rs := range got {
		a := &setAnswer{}
		for _, r := range rs {
			switch {
			case r.Error != nil:
				a.err = r.Error
			case r.Result != nil && r.Result.Match != nil && r.Result.Match.Done:
				a.marker = r.Result.Match
			case r.Result != nil && r.Result.Match != nil:
				a.members = append(a.members, r.Result.Match)
			}
		}
		answers[id] = a
	}
	if a := answers["container"]; a == nil || a.err == nil {
		t.Errorf("the container itself was read within an 8 KiB budget: %+v", a)
	}
	// The status of every job, and nothing read to find them.
	status := mustSet(t, answers, "status")
	if len(status.members) != n {
		t.Errorf("..status answered %d members, want %d", len(status.members), n)
	}
	for _, m := range status.members {
		if m.Body != nil || !strings.HasPrefix(m.Path, "jobs.j") {
			t.Errorf("..status answered %+v", m)
			break
		}
	}
	// Everything, with its kind: the root, four containers, and each record with its
	// fields -- n*(1+2) jobs, n*(1+1) nums, n*(1+1) list, n*(1+2) runs.
	all := mustSet(t, answers, "all")
	if want := 1 + 4 + 3*n + 2*n + 2*n + 3*n; len(all.members) != want {
		t.Errorf("`..` answered %d members, want %d", len(all.members), want)
	}
	kinds := map[string]string{}
	for _, m := range all.members {
		kinds[m.Path] = m.IterType
	}
	for path, want := range map[string]string{
		"": api.IterObject, "jobs": api.IterObject, "list": api.IterArray, "nums": api.IterSparseArray,
		"runs": api.IterKeyedArray, "jobs.j007": api.IterObject, "jobs.j007.status": api.IterString,
		"list[3]": api.IterObject, "nums{5}": api.IterObject, `runs."(id=r009)"`: api.IterObject,
		`runs."(id=r009)".id`: api.IterString,
	} {
		if got := kinds[path]; got != want {
			t.Errorf("%q: iterType %q, want %q", path, got, want)
		}
	}
	// From a named prefix: the container itself, then everything under it.
	under := mustSet(t, answers, "under")
	if len(under.members) != 1+3*n || under.members[0].Path != "jobs" {
		t.Errorf("jobs.. answered %d members starting %v, want %d with jobs first", len(under.members), under.paths()[:min(2, len(under.members))], 1+3*n)
	}
	// A body is a read per member, each within the budget, whatever the container's size.
	notes := mustSet(t, answers, "notes")
	if len(notes.members) != 4*n {
		t.Errorf("..note answered %d members, want %d", len(notes.members), 4*n)
	}
	for _, m := range notes.members {
		if m.Body == nil || m.Body.String != pad {
			t.Errorf("..note answered %s with body %v", m.Path, m.Body)
			break
		}
	}
}

// pageAll reads the set pattern names a page at a time, at limit per page, and answers
// the paths of every page together, with the commit the pages were read at.
func pageAll(t *testing.T, store *storage.Storage, pattern string, limit int) ([]string, int64) {
	t.Helper()
	var seen []string
	cursor := ""
	commit := int64(-1)
	for page := 1; ; page++ {
		req := fmt.Sprintf(`{id: "p", match: {path: %q, return: path, limit: %d`, pattern, limit)
		if cursor != "" {
			req += fmt.Sprintf(`, cursor: %q`, cursor)
		}
		p := mustSet(t, runSet(t, store, req+"}}"), "p")
		if commit >= 0 && p.marker.Commit != commit {
			t.Fatalf("%q page %d read at commit %d, page 1 at %d", pattern, page, p.marker.Commit, commit)
		}
		commit = p.marker.Commit
		if len(p.members) > limit {
			t.Fatalf("%q page %d answered %d members over a limit of %d", pattern, page, len(p.members), limit)
		}
		seen = append(seen, p.paths()...)
		cursor = p.marker.Cursor
		if cursor == "" {
			return seen, commit
		}
		if len(p.members) == 0 {
			t.Fatalf("%q page %d answered nothing while a cursor said there was more", pattern, page)
		}
		if page > 100 {
			t.Fatalf("%q: still paging after %d pages", pattern, page)
		}
	}
}

// TestSetMatch_DescentPaging: a descent pages as any set does, and a page boundary may
// fall anywhere in the walk -- between a node and its first child, between the last
// node under one container and the next container, at the end of the deepest branch.
// The pages together are the unpaged answer, in its order, whatever the page size, and
// a resume is a seek: the cursor's branch is followed down and the walk goes on from
// where the page ended, at the commit the first page read.
func TestSetMatch_DescentPaging(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", descentDoc)
	for _, pattern := range []string{"..", "..c", "a..", "a..b.*", "a.b..", "..[*]", "a....c", "a.x[*].."} {
		whole := mustSet(t, runSet(t, store, `{id: "w", match: {path: "`+pattern+`", return: path}}`), "w").paths()
		if len(whole) == 0 {
			t.Fatalf("%q answered nothing; the case proves nothing", pattern)
		}
		for _, limit := range []int{1, 2, 3, 5} {
			paged, _ := pageAll(t, store, pattern, limit)
			if !equalStrings(paged, whole) {
				t.Errorf("%q by pages of %d answered %v, want %v", pattern, limit, paged, whole)
			}
		}
	}
}

// TestSetMatch_DescentPagingReadsTheCursorsCommit: a write between two pages of a
// descent does not change what the later pages answer, wherever in the tree it lands.
func TestSetMatch_DescentPagingReadsTheCursorsCommit(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", descentDoc)
	whole := mustSet(t, runSet(t, store, `{id: "w", match: {path: "..c", return: path}}`), "w").paths()
	p1 := mustSet(t, runSet(t, store, `{id: "p1", match: {path: "..c", return: path, limit: 2}}`), "p1")
	if p1.marker.Cursor == "" {
		t.Fatal("page 1 says the set is finished, and it is not")
	}
	// A c before everything the walk has left, one on the branch the cursor is on, and
	// one after: none is this read's.
	rest := runSet(t, store,
		`{id: "write", patch: {path: "", data: {a: {"0": {c: 0}, b: {c: {c: 9}}, zz: {c: 10}}}}}`,
		`{id: "p2", match: {path: "..c", return: path, cursor: "`+p1.marker.Cursor+`"}}`,
		`{id: "now", match: {path: "..c", return: path}}`,
	)
	p2 := mustSet(t, rest, "p2")
	if got := append(append([]string{}, p1.paths()...), p2.paths()...); !equalStrings(got, whole) {
		t.Errorf("the pages answered %v, want %v: a page reads the commit its cursor names", got, whole)
	}
	if p2.marker.Commit != p1.marker.Commit {
		t.Errorf("page 2 read at commit %d, page 1 at %d", p2.marker.Commit, p1.marker.Commit)
	}
	if now := mustSet(t, rest, "now").paths(); len(now) != len(whole)+3 {
		t.Errorf("a fresh read after the write answered %v", now)
	}
}

// TestSetMatch_DescentKeyedAtItsCommit: a descent read at a commit keys arrays by the
// schema in force at that commit, wherever in the tree the arrays are. After the array
// loses its identity, a read at the commit before still names its elements by (*), and
// a read now names positions by [*].
func TestSetMatch_DescentKeyedAtItsCommit(t *testing.T) {
	store := openStore(t)
	keyed, err := parse.Parse([]byte(`{define: {deep: {runs: {id: !logd-key null}}}}`))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, err := store.SetSchema(keyed, false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	narrowWrite(t, store, "", `{deep: {runs: [{id: r1, n: 1}, {id: r2, n: 2}]}}`)
	then, err := store.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	unkeyed, err := parse.Parse([]byte(`{define: {deep: {runs: {id: null}}}}`))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, err := store.SetSchema(unkeyed, true); err != nil {
		t.Fatalf("SetSchema (losing identity): %v", err)
	}

	at := fmt.Sprint(then)
	answers := runSet(t, store,
		`{id: "then-keys", match: {path: "..(*)", commit: `+at+`, return: path}}`,
		`{id: "then-positions", match: {path: "..[*]", commit: `+at+`, return: path}}`,
		`{id: "then-kind", match: {path: "..runs", commit: `+at+`, return: "path,iterType"}}`,
		`{id: "now-keys", match: {path: "..(*)", return: path}}`,
		`{id: "now-positions", match: {path: "..[*]", return: path}}`,
		`{id: "now-kind", match: {path: "..runs", return: "path,iterType"}}`,
	)
	if got, want := mustSet(t, answers, "then-keys").paths(), []string{`deep.runs."(id=r1)"`, `deep.runs."(id=r2)"`}; !equalStrings(got, want) {
		t.Errorf("..(*) at the keyed commit answered %v, want %v", got, want)
	}
	if got := mustSet(t, answers, "then-positions").paths(); len(got) != 0 {
		t.Errorf("..[*] at the keyed commit answered %v, want nothing", got)
	}
	if got := mustSet(t, answers, "now-keys").paths(); len(got) != 0 {
		t.Errorf("..(*) after the identity was lost answered %v, want nothing", got)
	}
	if got, want := mustSet(t, answers, "now-positions").paths(), []string{"deep.runs[0]", "deep.runs[1]"}; !equalStrings(got, want) {
		t.Errorf("..[*] after the identity was lost answered %v, want %v", got, want)
	}
	for id, want := range map[string]string{"then-kind": api.IterKeyedArray, "now-kind": api.IterArray} {
		ms := mustSet(t, answers, id).members
		if len(ms) != 1 || ms[0].IterType != want {
			t.Errorf("%s: ..runs answered %+v, want one %s", id, ms, want)
		}
	}
}
