package server

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
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
	return runSetWith(t, &SessionConfig{Storage: store, Hub: NewWatchHub()}, requests...)
}

// runSetWith is runSet over a session configured as cfg says.
func runSetWith(t *testing.T, cfg *SessionConfig, requests ...string) map[string]*setAnswer {
	t.Helper()
	conn := newMockConn()
	for _, req := range requests {
		conn.WriteRequest(req)
	}
	session := NewSession("test-server", conn, cfg)
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

// TestSetMatch_Return covers the retspec: what an answer carries, from the three ways
// it can say a node. `return: path` is where it is, `return: id` the name it lives
// under, `return: body` what is in it -- and a spec with no body reads no node at all
// when there is no pattern to make it.
func TestSetMatch_Return(t *testing.T) {
	store := openStore(t)
	answers := runSet(t, store,
		`{id: "seed", patch: {path: "", data: {jobs: {a1: {status: done}, a2: {status: ready}}, leaf: 1}}}`,
		`{id: "paths", match: {path: "jobs.*", return: path}}`,
		`{id: "ids", match: {path: "jobs.*", return: id}}`,
		`{id: "id-body", match: {path: "jobs.*", return: "id,body"}}`,
		`{id: "bodies", match: {path: "jobs.*", return: body}}`,
		`{id: "both", match: {path: "jobs.*", return: "path,body"}}`,
		`{id: "filtered-paths", match: {path: "jobs.*", data: {status: done}, return: path}}`,
		`{id: "under-paths", match: {path: "jobs.*.status", return: path}}`,
		`{id: "absent-under", match: {path: "jobs.*.nope", return: path}}`,
		`{id: "one-node", match: {path: "leaf", return: path}}`,
		`{id: "unknown", match: {path: "jobs.*", return: "path,authors"}}`,
	)

	paths := mustSet(t, answers, "paths")
	if want := []string{"jobs.a1", "jobs.a2"}; !equalStrings(paths.paths(), want) {
		t.Errorf("return: path answered %v, want %v", paths.paths(), want)
	}
	for _, m := range paths.members {
		if m.Body != nil || m.ID != "" {
			t.Errorf("return: path carried more than the path for %s: %+v", m.Path, m)
		}
	}

	// The name a node lives under, without the prefix the caller already knows.
	ids := mustSet(t, answers, "ids")
	var got []string
	for _, m := range ids.members {
		got = append(got, m.ID)
		if m.Path != "" || m.Body != nil {
			t.Errorf("return: id carried more than the id: %+v", m)
		}
	}
	if want := []string{"a1", "a2"}; !equalStrings(got, want) {
		t.Errorf("return: id answered %v, want %v", got, want)
	}

	// A listing: the name, and what is under it.
	for _, m := range mustSet(t, answers, "id-body").members {
		if m.ID == "" || m.Body == nil || m.Path != "" {
			t.Errorf(`return: "id,body" answered %+v`, m)
		}
	}

	// Bodies alone: nobody can tell them apart, which is what a cumulative read wants.
	bodies := mustSet(t, answers, "bodies")
	if len(bodies.members) != 2 {
		t.Fatalf("return: body answered %d members", len(bodies.members))
	}
	for _, m := range bodies.members {
		if m.Path != "" || m.ID != "" {
			t.Errorf("return: body named the member: %+v", m)
		}
		if m.Body == nil {
			t.Error("return: body carried no body")
		}
	}

	both := mustSet(t, answers, "both")
	for _, m := range both.members {
		if m.Path == "" || m.Body == nil {
			t.Errorf(`return: "path,body" answered %+v`, m)
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
		t.Errorf("return: path at one node answered %+v", one.members)
	}

	// A name this server does not know is said, not ignored: a client asking a later
	// server for more must not be answered with less and told nothing.
	if a := answers["unknown"]; a == nil || a.err == nil {
		t.Error("an unknown return name was accepted")
	} else if a.err.Code != api.ErrCodeUnsupported {
		t.Errorf("unknown return name: code %q, want %q", a.err.Code, api.ErrCodeUnsupported)
	}
}

// schemaTransition opens a store under the schema from, writes runs under it, and sets the
// schema to, answering the store and the commit the write took -- the last commit read
// under from.
func schemaTransition(t *testing.T, from, to string, force bool) (*storage.Storage, int64) {
	t.Helper()
	store := openStore(t)
	for i, doc := range []string{from, to} {
		if i == 1 {
			narrowWrite(t, store, "", `{runs: [{id: r1, sku: A, n: 1}, {id: r2, sku: B, n: 2}]}`)
		}
		schema, err := parse.Parse([]byte(doc))
		if err != nil {
			t.Fatalf("parse schema %s: %v", doc, err)
		}
		if i == 1 {
			then, err := store.GetCurrentCommit()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetSchema(schema, force); err != nil {
				t.Fatalf("SetSchema %s: %v", doc, err)
			}
			return store, then
		}
		if _, err := store.SetSchema(schema, false); err != nil {
			t.Fatalf("SetSchema %s: %v", doc, err)
		}
	}
	panic("unreachable")
}

// historyRead is one read of a schema transition, and what it must answer.
type historyRead struct {
	name  string
	match string // the match request's body, without its id or commit
	head  bool   // read at the head rather than at the commit before the change
	check func(t *testing.T, a *setAnswer)
}

func runHistory(t *testing.T, store *storage.Storage, then int64, reads []historyRead) {
	t.Helper()
	requests := make([]string, 0, len(reads))
	for _, r := range reads {
		at := ""
		if !r.head {
			at = ", commit: " + strconv.FormatInt(then, 10)
		}
		requests = append(requests, `{id: "`+r.name+`", match: {`+r.match+at+`}}`)
	}
	answers := runSet(t, store, requests...)
	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			a := answers[r.name]
			if a == nil {
				t.Fatalf("%s was not answered", r.name)
			}
			r.check(t, a)
		})
	}
}

// anArray: the one node answered is the runs array, both elements, in array form.
func anArray(t *testing.T, a *setAnswer) {
	t.Helper()
	if a.err != nil || len(a.members) != 1 {
		t.Errorf("answered %+v", a)
		return
	}
	if b := a.members[0].Body; b == nil || b.Type != ir.ArrayType || len(b.Values) != 2 {
		t.Errorf("body is not the two-element array: %v", b)
	}
}

// element answers a check that the one node answered is the element whose id is id.
func element(id string) func(*testing.T, *setAnswer) {
	return func(t *testing.T, a *setAnswer) {
		t.Helper()
		if a.err != nil || len(a.members) != 1 {
			t.Errorf("answered %+v, want element %s", a, id)
			return
		}
		got, err := a.members[0].Body.GetKPath("id")
		if err != nil || got == nil || got.String != id {
			t.Errorf("body %v is not element %s", a.members[0].Body, id)
		}
	}
}

// refused answers a check that the read was refused with code.
func refused(code string) func(*testing.T, *setAnswer) {
	return func(t *testing.T, a *setAnswer) {
		t.Helper()
		if a.err == nil || a.err.Code != code {
			t.Errorf("answered %+v, want %s", a, code)
		}
	}
}

// bothNs: the set answered is the n of both elements.
func bothNs(t *testing.T, a *setAnswer) {
	t.Helper()
	if a.err != nil || a.marker == nil || len(a.members) != 2 {
		t.Errorf("answered %+v, want the n of both elements", a)
	}
}

// TestMatch_ReadsUnderTheSchemaOfItsCommit: a read at a commit reads the document as it
// was, under the schema in force then -- the array's shape, and the path that names an
// element -- whatever the schema has said since. Each transition is read at the commit
// before the change and at the head (3n390bjwh12ksy61n9n0).
func TestMatch_ReadsUnderTheSchemaOfItsCommit(t *testing.T) {
	const (
		keyedID  = `{define: {runs: {id: !logd-key null}}}`
		keyedSKU = `{define: {runs: {sku: !logd-key null}}}`
		unkeyed  = `{define: {runs: {id: null}}}`
	)

	t.Run("lose", func(t *testing.T) {
		store, then := schemaTransition(t, keyedID, unkeyed, true)
		runHistory(t, store, then, []historyRead{
			// Nothing is keyed now, so only the commit's schema says to raise.
			{name: "then-array", match: `path: runs`, check: anArray},
			{name: "then-set-body", match: `path: "*", return: "path,body"`, check: func(t *testing.T, a *setAnswer) {
				if a.err != nil || len(a.members) != 1 || a.members[0].Body == nil || a.members[0].Body.Type != ir.ArrayType {
					t.Errorf(`* with bodies at the keyed commit answered %+v`, a)
				}
			}},
			{name: "then-key", match: `path: "runs(r1)"`, check: element("r1")},
			{name: "then-position", match: `path: "runs[0]"`, check: refused(api.ErrCodeInvalidPath)},
			{name: "then-under", match: `path: "runs(*).n"`, check: bothNs},
			{name: "head-array", match: `path: runs`, head: true, check: anArray},
			{name: "head-key", match: `path: "runs(r1)"`, head: true, check: refused(api.ErrCodeInvalidPath)},
			{name: "head-position", match: `path: "runs[0]"`, head: true, check: element("r1")},
			// The path cannot be judged until the commit it is read at is known: today's
			// schema refuses runs(r1), and the commit is refused first.
			{name: "bad-commit", match: `path: "runs(r1)", commit: 999`, head: true, check: refused(api.ErrCodeCommitNotFound)},
		})
	})

	t.Run("gain", func(t *testing.T) {
		store, then := schemaTransition(t, unkeyed, keyedID, false)
		runHistory(t, store, then, []historyRead{
			{name: "then-array", match: `path: runs`, check: anArray},
			{name: "then-position", match: `path: "runs[0]"`, check: element("r1")},
			{name: "then-key", match: `path: "runs(r1)"`, check: refused(api.ErrCodeInvalidPath)},
			{name: "then-under", match: `path: "runs[*].n"`, check: bothNs},
			{name: "head-key", match: `path: "runs(r1)"`, head: true, check: element("r1")},
			{name: "head-position", match: `path: "runs[0]"`, head: true, check: refused(api.ErrCodeInvalidPath)},
			{name: "head-under", match: `path: "runs(*).n"`, head: true, check: bothNs},
			// No schema at all at commit 0: (r1) names nothing there.
			{name: "commit-0", match: `path: "runs(r1)", commit: 0`, head: true, check: refused(api.ErrCodeInvalidPath)},
		})
	})

	t.Run("re-key", func(t *testing.T) {
		store, then := schemaTransition(t, keyedID, keyedSKU, false)
		runHistory(t, store, then, []historyRead{
			{name: "then-array", match: `path: runs`, check: anArray},
			{name: "then-key", match: `path: "runs(r1)"`, check: element("r1")},
			{name: "then-other-key", match: `path: "runs(sku=A)"`, check: refused(api.ErrCodeInvalidPath)},
			{name: "then-under", match: `path: "runs(*).n"`, check: bothNs},
			{name: "head-key", match: `path: "runs(A)"`, head: true, check: element("r1")},
			{name: "head-other-key", match: `path: "runs(id=r1)"`, head: true, check: refused(api.ErrCodeInvalidPath)},
			{name: "head-under", match: `path: "runs(*).n"`, head: true, check: bothNs},
		})
	})
}

// TestMatch_ReturnOnBuiltNode: a single-node read answers what its retspec asks when it
// builds the node -- to raise it, on a store whose schema keys an array, or to filter it
// by a pattern -- as it does when it streams the body or answers an existence question
// (rvjwtghsh12krpnrn9n0).
func TestMatch_ReturnOnBuiltNode(t *testing.T) {
	store := openStore(t)
	schema, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}}}`))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	narrowWrite(t, store, "", `{runs: [{id: r1, n: 1}], leaf: {a: 1}}`)

	answers := runSet(t, store,
		`{id: "raised", match: {path: "leaf", return: "path,body"}}`,
		`{id: "filtered", match: {path: "leaf", data: {a: 1}, return: path}}`,
		`{id: "filtered-id-body", match: {path: "runs(r1)", data: {n: 1}, return: "id,body"}}`,
	)
	one := func(id string) *api.MatchResult {
		a := answers[id]
		if a == nil || a.err != nil || len(a.members) != 1 {
			t.Fatalf("%s: %+v", id, a)
		}
		return a.members[0]
	}
	if m := one("raised"); m.Path != "leaf" || m.Body == nil {
		t.Errorf(`return: "path,body" on a keyed store answered path %q, body %v`, m.Path, m.Body)
	}
	if m := one("filtered"); m.Path != "leaf" || m.Body != nil {
		t.Errorf(`return: path under a pattern answered path %q, body %v`, m.Path, m.Body)
	}
	if m := one("filtered-id-body"); m.ID != "(id=r1)" || m.Body == nil || m.Path != "" {
		t.Errorf(`return: "id,body" under a pattern answered %+v`, m)
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
		`{id: "ids", match: {path: "runs(*)", return: id}}`,
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

	// An element's id is the segment a client writes for it -- the identity, bound to its
	// field -- so the id says the array is keyed and by what, and runs + id is a path.
	var ids []string
	for _, m := range mustSet(t, answers, "ids").members {
		ids = append(ids, m.ID)
	}
	if want := []string{"(id=r1)", "(id=r2)"}; !equalStrings(ids, want) {
		t.Errorf("runs(*) ids = %v, want %v", ids, want)
	}
	byID := runSet(t, store, `{id: "one", match: {path: "runs`+ids[0]+`"}}`)
	if a := byID["one"]; a == nil || a.err != nil || len(a.members) != 1 {
		t.Errorf("runs%s does not read: %+v", ids[0], a)
	} else if got, err := a.members[0].Body.GetKPath("id"); err != nil || got == nil || got.String != "r1" {
		t.Errorf("runs%s read %v", ids[0], a.members[0].Body)
	}
}

// TestSetMatch_KeyedAtItsCommit: a set read at a commit keys arrays by the schema in
// force at that commit, as a read of that commit raises them. After the array loses its
// identity, a read at the commit before still names its elements by (*), and a read now
// names positions by [*] (xnepz3sfh12ksn5qn9n0).
func TestSetMatch_KeyedAtItsCommit(t *testing.T) {
	store := openStore(t)
	keyed, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}}}`))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, err := store.SetSchema(keyed, false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	narrowWrite(t, store, "", `{runs: [{id: r1, n: 1}, {id: r2, n: 2}]}`)
	then, err := store.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	unkeyed, err := parse.Parse([]byte(`{define: {runs: {id: null}}}`))
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	if _, err := store.SetSchema(unkeyed, true); err != nil {
		t.Fatalf("SetSchema (losing identity): %v", err)
	}

	at := strconv.FormatInt(then, 10)
	answers := runSet(t, store,
		`{id: "then-keys", match: {path: "runs(*)", commit: `+at+`, return: id}}`,
		`{id: "then-positions", match: {path: "runs[*]", commit: `+at+`, return: id}}`,
		`{id: "now-keys", match: {path: "runs(*)", return: id}}`,
		`{id: "now-positions", match: {path: "runs[*]", return: id}}`,
	)
	ids := func(id string) []string {
		var out []string
		for _, m := range mustSet(t, answers, id).members {
			out = append(out, m.ID)
		}
		return out
	}
	if got, want := ids("then-keys"), []string{"(id=r1)", "(id=r2)"}; !equalStrings(got, want) {
		t.Errorf("runs(*) at the keyed commit answered %v, want %v", got, want)
	}
	if got := ids("then-positions"); len(got) != 0 {
		t.Errorf("runs[*] at the keyed commit answered %v, want nothing", got)
	}
	if got := ids("now-keys"); len(got) != 0 {
		t.Errorf("runs(*) after the identity was lost answered %v, want nothing", got)
	}
	if got, want := ids("now-positions"), []string{"[0]", "[1]"}; !equalStrings(got, want) {
		t.Errorf("runs[*] after the identity was lost answered %v, want %v", got, want)
	}
}

// TestMemberID is the id for each kind of member: the segment a client would write for it
// -- a field, a position, a sparse key, a keyed element's identity -- so the id says which
// kind of child it is, and the prefix and the id together are a path (eavavw16h12kst8dndn0).
func TestMemberID(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"jobs.a1", "a1"},
		{`jobs."a b"`, `"a b"`},
		{"list[2]", "[2]"},
		{"events{7}", "{7}"},
		{`runs."(id=r1)"`, "(id=r1)"},
		{`runs."(sku=\"42\")"`, `(sku="42")`}, // a string that would read as a number keeps its quotes
		{`runs."<{region: eu, sku: A}>"`, `(region=eu,sku=A)`},
		{`runs."<{region: eu, sku: \"a,b\"}>"`, `'<{region: eu, sku: "a,b"}>'`}, // no key segment carries a comma: the field, as kpath quotes it
		{"", ""},
	} {
		if got := memberID(tc.path); got != tc.want {
			t.Errorf("memberID(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestSetMatch_IterType: `return: iterType` says what kind each member is, in the terms
// a client lists by, and the wildcard IterWildcard gives for it lists exactly that node's
// children -- which is the point: the answer alone says what to list next and how
// (1k6w71sfh12ksn5qn9n0). A sparse array is not an Object, a keyed array is not an Array,
// and an element of a keyed array is not a keyed array.
func TestSetMatch_IterType(t *testing.T) {
	const doc = `{o: {a: 1, b: 2}, eo: {}, sp: !sparsearray {3: x, 7: y}, esp: !sparsearray {}, ` +
		`arr: [1, 2, 3], earr: [], str: x, num: 1, flt: 1.5, yes: true, nul: null}`
	want := map[string]string{
		"o": api.IterObject, "eo": api.IterObject,
		"sp": api.IterSparseArray, "esp": api.IterSparseArray,
		"arr": api.IterArray, "earr": api.IterArray,
		"str": api.IterString, "num": api.IterNumber, "flt": api.IterNumber,
		"yes": api.IterBool, "nul": api.IterNull,
	}
	children := map[string]int{"o": 2, "sp": 2, "arr": 3}

	// kinds answers a set's members by id, and checks each container's listing.
	kinds := func(t *testing.T, store *storage.Storage, answers map[string]*setAnswer, id string, prefix string) map[string]string {
		t.Helper()
		out := map[string]string{}
		var listings []string
		for _, m := range mustSet(t, answers, id).members {
			out[m.ID] = m.IterType
			if w := api.IterWildcard(m.IterType); w != "" {
				listings = append(listings, `{id: "list `+m.ID+`", match: {path: "`+kpathJoin(prefix, m.ID)+w+`", return: id}}`)
			}
		}
		listed := runSet(t, store, listings...)
		for name, n := range children {
			if got := len(mustSet(t, listed, "list "+name).members); got != n {
				t.Errorf("%s: listing %s by %s answered %d children, want %d",
					id, name, api.IterWildcard(out[name]), got, n)
			}
		}
		return out
	}

	// No schema: a body is streamed, and iterType rides the encoded frame.
	t.Run("streamed", func(t *testing.T) {
		store := openStore(t)
		narrowWrite(t, store, "", doc)
		answers := runSet(t, store,
			`{id: "kinds", match: {path: "*", return: "id,iterType"}}`,
			`{id: "with-body", match: {path: "*", return: "id,iterType,body"}}`,
			`{id: "one", match: {path: "sp", return: iterType}}`,
			`{id: "absent", match: {path: "nope", return: iterType}}`,
		)
		for name, got := range kinds(t, store, answers, "kinds", "") {
			if got != want[name] {
				t.Errorf("%s: iterType %q, want %q", name, got, want[name])
			}
		}
		for _, m := range mustSet(t, answers, "with-body").members {
			if m.IterType != want[m.ID] || m.Body == nil {
				t.Errorf(`return: "id,iterType,body" answered %s: iterType %q, body %v`, m.ID, m.IterType, m.Body)
			}
		}
		if a := answers["one"]; a == nil || a.err != nil || len(a.members) != 1 || a.members[0].IterType != api.IterSparseArray || a.members[0].Body != nil {
			t.Errorf("return: iterType at one node answered %+v", a)
		}
		if a := answers["absent"]; a == nil || a.err == nil || a.err.Code != api.ErrCodeNotFound {
			t.Errorf("return: iterType where nothing stands answered %+v, want not_found", a)
		}
	})

	// A schema keying an array: bodies are built and raised, and the keyed array is its
	// own kind -- at the commit read, by the schema in force then.
	t.Run("keyed", func(t *testing.T) {
		store := openStore(t)
		keyed, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}}}`))
		if err != nil {
			t.Fatalf("parse schema: %v", err)
		}
		if _, err := store.SetSchema(keyed, false); err != nil {
			t.Fatalf("SetSchema: %v", err)
		}
		narrowWrite(t, store, "", `{runs: [{id: r1, n: 1}, {id: r2, n: 2}]}`)
		narrowWrite(t, store, "", doc)
		then, err := store.GetCurrentCommit()
		if err != nil {
			t.Fatal(err)
		}
		want["runs"] = api.IterKeyedArray
		children["runs"] = 2
		defer delete(want, "runs")
		defer delete(children, "runs")

		answers := runSet(t, store,
			`{id: "kinds", match: {path: "*", return: "id,iterType"}}`,
			`{id: "with-body", match: {path: "*", return: "id,iterType,body"}}`,
			`{id: "elements", match: {path: "runs(*)", return: "id,iterType,body"}}`,
			`{id: "one", match: {path: "runs", data: {}, return: "iterType"}}`,
		)
		for name, got := range kinds(t, store, answers, "kinds", "") {
			if got != want[name] {
				t.Errorf("%s: iterType %q, want %q", name, got, want[name])
			}
		}
		for _, m := range mustSet(t, answers, "with-body").members {
			if m.IterType != want[m.ID] || m.Body == nil {
				t.Errorf(`return: "id,iterType,body" answered %s: iterType %q, body %v`, m.ID, m.IterType, m.Body)
			}
		}
		for _, m := range mustSet(t, answers, "elements").members {
			if m.IterType != api.IterObject {
				t.Errorf("element %s of a keyed array: iterType %q, want %q", m.ID, m.IterType, api.IterObject)
			}
		}
		if a := answers["one"]; a == nil || a.err != nil || len(a.members) != 1 || a.members[0].IterType != api.IterKeyedArray {
			t.Errorf("return: iterType at a keyed array under a pattern answered %+v", a)
		}

		unkeyed, err := parse.Parse([]byte(`{define: {runs: {id: null}}}`))
		if err != nil {
			t.Fatalf("parse schema: %v", err)
		}
		if _, err := store.SetSchema(unkeyed, true); err != nil {
			t.Fatalf("SetSchema (losing identity): %v", err)
		}
		at := strconv.FormatInt(then, 10)
		history := runSet(t, store,
			`{id: "then", match: {path: "runs", commit: `+at+`, return: iterType}}`,
			`{id: "now", match: {path: "runs", return: iterType}}`,
		)
		for id, kind := range map[string]string{"then": api.IterKeyedArray, "now": api.IterArray} {
			if a := history[id]; a == nil || a.err != nil || len(a.members) != 1 || a.members[0].IterType != kind {
				t.Errorf("runs %s: answered %+v, want iterType %q", id, a, kind)
			}
		}
	})
}

// kpathJoin appends a member's id to the prefix it was listed under, as a field.
func kpathJoin(prefix, id string) string {
	if prefix == "" {
		return id
	}
	return prefix + "." + id
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

// TestSetMatch_ListsBeyondTheReadBudget: a wildcard level is enumerated from the store's
// events, a name at a time, so a container larger than the session's read budget is
// listed -- and its members read -- where a read of the container itself is refused
// (3kgxprskh12krjrmndn0). Every kind of level: fields, a sparse array, a dense array, and
// a keyed array.
func TestSetMatch_ListsBeyondTheReadBudget(t *testing.T) {
	store := openStore(t)
	schema, err := parse.Parse([]byte(`{define: {runs: {id: !logd-key null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	const n = 200
	pad := strings.Repeat("x", 120)
	var jobs, nums, list, runs []string
	for i := range n {
		jobs = append(jobs, fmt.Sprintf("j%03d: {status: done, note: %s}", i, pad))
		nums = append(nums, fmt.Sprintf("%d: {note: %s}", i, pad))
		list = append(list, fmt.Sprintf("{note: %s}", pad))
		runs = append(runs, fmt.Sprintf("{id: r%03d, note: %s}", i, pad))
	}
	narrowWrite(t, store, "", "{jobs: {"+strings.Join(jobs, ", ")+"}, nums: !sparsearray {"+strings.Join(nums, ", ")+
		"}, list: ["+strings.Join(list, ", ")+"], runs: ["+strings.Join(runs, ", ")+"]}")

	answers := runSetWith(t, &SessionConfig{Storage: store, Hub: NewWatchHub(), ReadBudget: 8 << 10},
		`{id: "container", match: {path: jobs}}`,
		`{id: "jobs", match: {path: "jobs.*", return: path}}`,
		`{id: "jobs-bodies", match: {path: "jobs.*", return: "path,body"}}`,
		`{id: "nums", match: {path: "nums{*}", return: id}}`,
		`{id: "list", match: {path: "list[*]", return: id}}`,
		`{id: "runs", match: {path: "runs(*)", return: id}}`,
		`{id: "under", match: {path: "jobs.*.status", return: path}}`,
	)
	if a := answers["container"]; a == nil || a.err == nil {
		t.Errorf("the container itself was read within an 8 KiB budget: %+v", a)
	}
	for _, id := range []string{"jobs", "jobs-bodies", "nums", "list", "runs", "under"} {
		a := answers[id]
		if a == nil || a.err != nil {
			t.Errorf("%s: %+v", id, a)
			continue
		}
		if a.marker == nil || len(a.members) != n {
			t.Errorf("%s answered %d members, want %d", id, len(a.members), n)
		}
	}
	if a := answers["jobs-bodies"]; a != nil && a.err == nil && len(a.members) > 0 && a.members[0].Body == nil {
		t.Error("jobs.* with bodies answered no body")
	}
	if a := answers["runs"]; a != nil && a.err == nil && len(a.members) > 0 && a.members[0].ID != "(id=r000)" {
		t.Errorf("runs(*) first id = %q, want (id=r000)", a.members[0].ID)
	}
}
