package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// setAnswer is what a match answered: the members in the order they arrived, and the
// marker that ended them.
type setAnswer struct {
	members []*api.MatchResult
	marker  *api.MatchResult
	err     *api.SessionError
}

func (a setAnswer) paths() []string {
	out := make([]string, 0, len(a.members))
	for _, m := range a.members {
		out = append(out, m.Path)
	}
	return out
}

// runSet drives one session over the requests and collects what each id was answered.
func runSet(t *testing.T, store *storage.Storage, requests ...string) map[string]*setAnswer {
	t.Helper()
	conn := newMockConn()
	for _, req := range requests {
		conn.WriteRequest(req)
	}
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: NewWatchHub()})
	done := make(chan error)
	go func() { done <- session.Run() }()
	time.Sleep(100 * time.Millisecond)
	conn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session did not complete")
	}

	out := map[string]*setAnswer{}
	for _, line := range bytes.Split(bytes.TrimSpace(conn.GetResponses()), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp api.SessionResponse
		if err := resp.FromTony(bytes.TrimSpace(line)); err != nil {
			t.Fatalf("parse %s: %v", line, err)
		}
		if resp.ID == nil {
			continue
		}
		a := out[*resp.ID]
		if a == nil {
			a = &setAnswer{}
			out[*resp.ID] = a
		}
		switch {
		case resp.Error != nil:
			a.err = resp.Error
		case resp.Result != nil && resp.Result.Match != nil:
			if resp.Result.Match.Done {
				a.marker = resp.Result.Match
				continue
			}
			a.members = append(a.members, resp.Result.Match)
		}
	}
	return out
}

func mustSet(t *testing.T, answers map[string]*setAnswer, id string) *setAnswer {
	t.Helper()
	a := answers[id]
	if a == nil {
		t.Fatalf("%s was not answered", id)
	}
	if a.err != nil {
		t.Fatalf("%s: %s: %s", id, a.err.Code, a.err.Message)
	}
	if a.marker == nil {
		t.Fatalf("%s: the set was not ended by a marker", id)
	}
	return a
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSetMatch_Wildcards answers a set one node at a time: every member carries its own
// path and the one commit the set was read at, and the marker ends it
// (y2agz9dyh12kse24n9n0).
func TestSetMatch_Wildcards(t *testing.T) {
	store := openStore(t)
	answers := runSet(t, store,
		`{id: "seed", patch: {path: "", data: {jobs: {a1: {status: done}, a2: {status: ready}}, nums: !sparsearray {3: x, 7: y}, list: [10, 20], leaf: 1}}}`,
		`{id: "fields", match: {path: "jobs.*"}}`,
		`{id: "sparse", match: {path: "nums{*}"}}`,
		`{id: "dense", match: {path: "list[*]"}}`,
		`{id: "kind-mismatch", match: {path: "jobs[*]"}}`,
		`{id: "leaf-wild", match: {path: "leaf.*"}}`,
		`{id: "absent", match: {path: "nope.*"}}`,
		`{id: "under", match: {path: "jobs.*.status"}}`,
	)

	fields := mustSet(t, answers, "fields")
	if want := []string{"jobs.a1", "jobs.a2"}; !equalStrings(fields.paths(), want) {
		t.Errorf("jobs.* answered %v, want %v", fields.paths(), want)
	}
	commit := fields.marker.Commit
	for _, m := range fields.members {
		if m.Commit != commit {
			t.Errorf("member %s read at commit %d, and the set at %d", m.Path, m.Commit, commit)
		}
		if m.Body == nil {
			t.Errorf("member %s carries no body", m.Path)
		}
	}
	if fields.marker.Cursor != "" {
		t.Errorf("a finished set carries a cursor: %q", fields.marker.Cursor)
	}

	if got := mustSet(t, answers, "sparse").paths(); !equalStrings(got, []string{"nums{3}", "nums{7}"}) {
		t.Errorf("nums{*} answered %v", got)
	}
	if got := mustSet(t, answers, "dense").paths(); !equalStrings(got, []string{"list[0]", "list[1]"}) {
		t.Errorf("list[*] answered %v", got)
	}
	// A wildcard that meets a container of another kind, or a leaf, or nothing at all,
	// reaches nothing: an empty set is the marker alone.
	for _, id := range []string{"kind-mismatch", "leaf-wild", "absent"} {
		a := mustSet(t, answers, id)
		if len(a.members) != 0 {
			t.Errorf("%s answered %v, want nothing", id, a.paths())
		}
	}
	// A concrete segment after a wildcard is the general case.
	under := mustSet(t, answers, "under")
	if want := []string{"jobs.a1.status", "jobs.a2.status"}; !equalStrings(under.paths(), want) {
		t.Errorf("jobs.*.status answered %v, want %v", under.paths(), want)
	}
	if b := under.members[0].Body; b == nil || b.String != "done" {
		t.Errorf("jobs.a1.status body = %v, want done", b)
	}
}

// TestSetMatch_Return covers the retspec: what an answer carries. `return: paths`
// answers where the nodes are without sending what is in them -- and, with no pattern,
// without reading them; `return: body` answers the values alone.
func TestSetMatch_Return(t *testing.T) {
	store := openStore(t)
	answers := runSet(t, store,
		`{id: "seed", patch: {path: "", data: {jobs: {a1: {status: done}, a2: {status: ready}}, leaf: 1}}}`,
		`{id: "paths", match: {path: "jobs.*", return: paths}}`,
		`{id: "bodies", match: {path: "jobs.*", return: body}}`,
		`{id: "both", match: {path: "jobs.*", return: "paths,body"}}`,
		`{id: "filtered-paths", match: {path: "jobs.*", data: {status: done}, return: paths}}`,
		`{id: "under-paths", match: {path: "jobs.*.status", return: paths}}`,
		`{id: "absent-under", match: {path: "jobs.*.nope", return: paths}}`,
		`{id: "one-node", match: {path: "leaf", return: paths}}`,
		`{id: "unknown", match: {path: "jobs.*", return: "paths,authors"}}`,
	)

	paths := mustSet(t, answers, "paths")
	if want := []string{"jobs.a1", "jobs.a2"}; !equalStrings(paths.paths(), want) {
		t.Errorf("return: paths answered %v, want %v", paths.paths(), want)
	}
	for _, m := range paths.members {
		if m.Body != nil {
			t.Errorf("return: paths carried a body for %s: %v", m.Path, m.Body)
		}
	}

	bodies := mustSet(t, answers, "bodies")
	if len(bodies.members) != 2 {
		t.Fatalf("return: body answered %d members", len(bodies.members))
	}
	for _, m := range bodies.members {
		if m.Path != "" {
			t.Errorf("return: body carried a path: %s", m.Path)
		}
		if m.Body == nil {
			t.Error("return: body carried no body")
		}
	}

	both := mustSet(t, answers, "both")
	for _, m := range both.members {
		if m.Path == "" || m.Body == nil {
			t.Errorf(`return: "paths,body" answered %+v`, m)
		}
	}

	// A pattern still selects: the paths of the jobs that are done.
	if got := mustSet(t, answers, "filtered-paths").paths(); !equalStrings(got, []string{"jobs.a1"}) {
		t.Errorf("a filtered paths-only read answered %v", got)
	}
	// A concrete segment after the wildcard is a path the walk named rather than found,
	// so what is not there is not answered.
	if got := mustSet(t, answers, "under-paths").paths(); !equalStrings(got, []string{"jobs.a1.status", "jobs.a2.status"}) {
		t.Errorf("jobs.*.status paths answered %v", got)
	}
	if got := mustSet(t, answers, "absent-under").paths(); len(got) != 0 {
		t.Errorf("a path that is not there was answered: %v", got)
	}

	// A path naming one node: `return: paths` is an existence question.
	one := answers["one-node"]
	if one == nil || one.err != nil {
		t.Fatalf("one-node: %+v", one)
	}
	if len(one.members) != 1 || one.members[0].Path != "leaf" || one.members[0].Body != nil {
		t.Errorf("return: paths at one node answered %+v", one.members)
	}

	// A name this server does not know is said, not ignored: a client asking a later
	// server for more must not be answered with less and told nothing.
	if a := answers["unknown"]; a == nil || a.err == nil {
		t.Error("an unknown return name was accepted")
	} else if a.err.Code != api.ErrCodeUnsupported {
		t.Errorf("unknown return name: code %q, want %q", a.err.Code, api.ErrCodeUnsupported)
	}
}

// TestSetMatch_KeyedElements: (*) names a keyed array's elements by identity, which is
// how the store spells them and how a client addresses them, and [*] names nothing
// there -- identity replaces position, as it does for a read of one element.
func TestSetMatch_KeyedElements(t *testing.T) {
	store := openStore(t)
	schema, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}}}`))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	narrowWrite(t, store, "", `{runs: [{id: r1, n: 1}, {id: r2, n: 2}]}`)

	answers := runSet(t, store,
		`{id: "elements", match: {path: "runs(*)"}}`,
		`{id: "under", match: {path: "runs(*).n"}}`,
		`{id: "positions", match: {path: "runs[*]"}}`,
	)

	elements := mustSet(t, answers, "elements")
	want := []string{`runs."(id=r1)"`, `runs."(id=r2)"`}
	if !equalStrings(elements.paths(), want) {
		t.Errorf("runs(*) answered %v, want %v", elements.paths(), want)
	}
	// Every member path a set answers is a path a client can read on its own.
	back := runSet(t, store, `{id: "one", match: {path: "`+elements.paths()[0]+`"}}`)
	if a := back["one"]; a == nil || a.err != nil {
		t.Errorf("a member path is not readable on its own: %+v", a)
	}
	if got := mustSet(t, answers, "under").paths(); !equalStrings(got, []string{`runs."(id=r1)".n`, `runs."(id=r2)".n`}) {
		t.Errorf("runs(*).n answered %v", got)
	}
	if got := mustSet(t, answers, "positions").paths(); len(got) != 0 {
		t.Errorf("runs[*] over a keyed array answered %v, want nothing", got)
	}
}

// TestSetMatch_PatternAndHistory: the request's pattern selects and trims each member on
// its own, and a historical commit reads the set as it was.
func TestSetMatch_PatternAndHistory(t *testing.T) {
	store := openStore(t)
	answers := runSet(t, store,
		`{id: "seed", patch: {path: "", data: {jobs: {a1: {status: done, n: 1}, a2: {status: ready, n: 2}}}}}`,
		`{id: "done", match: {path: "jobs.*", data: {status: done}}}`,
		`{id: "trim", match: {path: "jobs.*", data: {n: !irtype 0}}}`,
		`{id: "add", patch: {path: "jobs.a3", data: {status: done, n: 3}}}`,
		`{id: "now", match: {path: "jobs.*", data: {status: done}}}`,
		`{id: "then", match: {path: "jobs.*", commit: 1, data: {status: done}}}`,
	)

	done := mustSet(t, answers, "done")
	if want := []string{"jobs.a1"}; !equalStrings(done.paths(), want) {
		t.Errorf("the pattern did not select: %v", done.paths())
	}
	trim := mustSet(t, answers, "trim")
	if len(trim.members) != 2 {
		t.Fatalf("trim answered %d members", len(trim.members))
	}
	if b := trim.members[0].Body; b == nil || len(b.Fields) != 1 || b.Fields[0].String != "n" {
		t.Errorf("the pattern did not trim the member: %v", b)
	}
	if got := mustSet(t, answers, "now").paths(); !equalStrings(got, []string{"jobs.a1", "jobs.a3"}) {
		t.Errorf("after the write: %v", got)
	}
	if got := mustSet(t, answers, "then").paths(); !equalStrings(got, []string{"jobs.a1"}) {
		t.Errorf("a read at commit 1 sees the write that came after it: %v", got)
	}
}

// TestSetMatch_Paging pages a set: the marker carries a cursor while more remains, the
// continuation answers the rest, and the pages together are the set, in order, with no
// member twice.
func TestSetMatch_Paging(t *testing.T) {
	store := openStore(t)
	answers := runSet(t, store,
		`{id: "seed", patch: {path: "", data: {jobs: {a: 1, b: 2, c: 3, d: 4, e: 5}}}}`,
		`{id: "p1", match: {path: "jobs.*", limit: 2}}`,
	)
	p1 := mustSet(t, answers, "p1")
	if want := []string{"jobs.a", "jobs.b"}; !equalStrings(p1.paths(), want) {
		t.Fatalf("page 1 answered %v, want %v", p1.paths(), want)
	}
	if p1.marker.Cursor == "" {
		t.Fatal("page 1 says the set is finished, and it is not")
	}

	// The continuation, and the one after it.
	seen := append([]string{}, p1.paths()...)
	cursor := p1.marker.Cursor
	for page := 2; page <= 4 && cursor != ""; page++ {
		next := runSet(t, store,
			`{id: "p", match: {path: "jobs.*", limit: 2, cursor: "`+cursor+`"}}`,
		)
		p := mustSet(t, next, "p")
		if len(p.members) == 0 {
			t.Fatalf("page %d answered nothing while a cursor said there was more", page)
		}
		seen = append(seen, p.paths()...)
		cursor = p.marker.Cursor
	}
	if cursor != "" {
		t.Errorf("the set never finished: %q", cursor)
	}
	want := []string{"jobs.a", "jobs.b", "jobs.c", "jobs.d", "jobs.e"}
	if !equalStrings(seen, want) {
		t.Errorf("the pages together answered %v, want %v", seen, want)
	}
}

// TestSetMatch_PagingReadsTheCursorsCommit: a write between two pages does not change
// what the second page answers, because a cursor names the commit its read started at.
func TestSetMatch_PagingReadsTheCursorsCommit(t *testing.T) {
	store := openStore(t)
	first := runSet(t, store,
		`{id: "seed", patch: {path: "", data: {jobs: {a: 1, b: 2, c: 3}}}}`,
		`{id: "p1", match: {path: "jobs.*", limit: 1}}`,
	)
	p1 := mustSet(t, first, "p1")
	if p1.marker.Cursor == "" {
		t.Fatal("page 1 says the set is finished, and it is not")
	}
	rest := runSet(t, store,
		`{id: "write", patch: {path: "jobs.b2", data: 9}}`,
		`{id: "p2", match: {path: "jobs.*", limit: 10, cursor: "`+p1.marker.Cursor+`"}}`,
	)
	p2 := mustSet(t, rest, "p2")
	if want := []string{"jobs.b", "jobs.c"}; !equalStrings(p2.paths(), want) {
		t.Errorf("page 2 answered %v, want %v: a page reads the commit its cursor names", p2.paths(), want)
	}
	if p2.marker.Commit != p1.marker.Commit {
		t.Errorf("page 2 read at commit %d, page 1 at %d", p2.marker.Commit, p1.marker.Commit)
	}
}

// TestSetMatch_CursorRefusals: a cursor continues the read that made it, at the commit
// it names, and says so when it cannot.
func TestSetMatch_CursorRefusals(t *testing.T) {
	store := openStore(t)
	first := runSet(t, store,
		`{id: "seed", patch: {path: "", data: {jobs: {a: 1, b: 2}}}}`,
		`{id: "p1", match: {path: "jobs.*", limit: 1}}`,
	)
	cursor := mustSet(t, first, "p1").marker.Cursor

	answers := runSet(t, store,
		`{id: "other-path", match: {path: "other.*", cursor: "`+cursor+`"}}`,
		`{id: "nonsense", match: {path: "jobs.*", cursor: "not-a-cursor!"}}`,
		`{id: "bad-limit", match: {path: "jobs.*", limit: 0}}`,
	)
	for _, id := range []string{"other-path", "nonsense", "bad-limit"} {
		a := answers[id]
		if a == nil || a.err == nil {
			t.Errorf("%s was not refused", id)
			continue
		}
		if a.err.Code != api.ErrCodeInvalidPath {
			t.Errorf("%s: code %q: %s", id, a.err.Code, a.err.Message)
		}
	}
}
