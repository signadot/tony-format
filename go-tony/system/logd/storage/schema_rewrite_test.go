package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A change to an array's identity is carried by the schema commit as a rewrite of the
// array (identityRewrite): the data and the schema move together, at one commit. Before,
// the change was refused while the array held anything, and a loss left the elements
// under their names with a schema that reads them as positions (090mbrhsh12ksfr8mhn0).

func keyedPathsAt(t *testing.T, s *Storage, prefix string) []string {
	t.Helper()
	var out []string
	for _, p := range indexPathSet(s) {
		if strings.HasPrefix(p, prefix) {
			out = append(out, p)
		}
	}
	return out
}

// GAIN: elements written by position are named from their key fields and held under the
// names; a later write names one and the others survive; the read answers the array.
func TestSchemaRewrite_GainNamesTheElements(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: [{sku: B, q: 2}, {sku: A, q: 1}], other: 1}`)
	commit := migrateTo(t, s, `{define: {items: {sku: !logd-key null}}}`)

	if paths := keyedPathsAt(t, s, `items."(sku=`); len(paths) < 2 {
		t.Errorf("the elements are not held under their names: %v", indexPathSet(s))
	}
	got := flatten(t, mustReadScope(t, s, commit, nil))
	if want := `items: - q: 1 sku: A - q: 2 sku: B other: 1`; got != want {
		t.Errorf("after the gain\n got %s\nwant %s", got, want)
	}
	// A write by name merges into one element; the other is not touched.
	c := mustCommit(t, s, nil, `{items: [{sku: A, q: 10}]}`)
	got = flatten(t, mustReadScope(t, s, c, nil))
	if want := `items: - q: 10 sku: A - q: 2 sku: B other: 1`; got != want {
		t.Errorf("after a write by name\n got %s\nwant %s", got, want)
	}
	// And the state before the schema commit is still what it was.
	if got := flatten(t, mustReadScope(t, s, commit-1, nil)); got != `items: [ { sku: B q: 2 } { sku: A q: 1 } ] other: 1` {
		t.Errorf("the state before the schema commit changed: %s", got)
	}
}

// GAIN of an auto-id: an element without an id is given one, minted from the commit.
func TestSchemaRewrite_GainGeneratesAutoIDs(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: [{q: 1}, {q: 2}]}`)
	commit := migrateTo(t, s, `{define: {items: {id: !logd-auto-id null}}}`)
	got := mustReadScope(t, s, commit, nil)
	items, _ := got.GetKPath("items")
	if items == nil || items.Type != ir.ArrayType || len(items.Values) != 2 {
		t.Fatalf("after the gain: %s", flatten(t, got))
	}
	seen := map[string]bool{}
	for i, el := range items.Values {
		id := getField(el, "id")
		if id == nil || id.Type != ir.StringType || id.String == "" {
			t.Errorf("element %d has no generated id: %s", i, flatten(t, el))
			continue
		}
		if seen[id.String] {
			t.Errorf("two elements share the id %s", id.String)
		}
		seen[id.String] = true
	}
}

// CHANGE: the elements are named again under the new key fields.
func TestSchemaRewrite_ChangeRenamesTheElements(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: A, code: x}, {sku: B, code: y}]}`)
	commit := migrateTo(t, s, `{define: {items: {code: !logd-key null}}}`)
	if paths := keyedPathsAt(t, s, `items."(code=`); len(paths) < 2 {
		t.Errorf("the elements are not held under their new names: %v", indexPathSet(s))
	}
	got := flatten(t, mustReadScope(t, s, commit, nil))
	if want := `items: - code: x sku: A - code: y sku: B`; got != want {
		t.Errorf("after the change\n got %s\nwant %s", got, want)
	}
	c := mustCommit(t, s, nil, `{items: [{code: y, sku: BB}]}`)
	if got := flatten(t, mustReadScope(t, s, c, nil)); got != `items: - code: x sku: A - code: y sku: BB` {
		t.Errorf("a write under the new name: %s", got)
	}
}

// LOSE: refused, unless forced; forced, the elements come back as an array in name order
// and positional writes work on it.
func TestSchemaRewrite_LossNeedsForceAndAnswersAnArray(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: B}, {sku: A}]}`)
	_, err := s.SetSchema(testSchema(t, `{define: {items: {sku: null}}}`), false)
	if err == nil || !IsSchemaRefused(err) || !strings.Contains(err.Error(), "force") {
		t.Fatalf("an unforced loss was answered %v", err)
	}
	commit, err := s.SetSchema(testSchema(t, `{define: {items: {sku: null}}}`), true)
	if err != nil {
		t.Fatalf("a forced loss was refused: %v", err)
	}
	if got := flatten(t, mustReadScope(t, s, commit, nil)); got != `items: - sku: A - sku: B` {
		t.Errorf("after the loss: %s", got)
	}
	if paths := keyedPathsAt(t, s, `items."(sku=`); len(paths) > 0 {
		// The old names are still in the index's history of paths; what matters is
		// that the array is read as one and written by position.
		t.Logf("named paths remain in the index (history): %v", paths)
	}
	c := mustCommit(t, s, nil, `{items: !arraydiff {1: {sku: C}}}`)
	if got := flatten(t, mustReadScope(t, s, c, nil)); got != `items: [ { sku: A } { sku: C } ]` {
		t.Errorf("a positional write after the loss: %s", got)
	}
}

// An element that cannot be named under the new identity refuses the change, naming it,
// and nothing changes.
func TestSchemaRewrite_UnnameableElementRefuses(t *testing.T) {
	for _, tc := range []struct{ name, seed, want string }{
		{"no key", `{items: [{sku: A}, {q: 2}]}`, "has no name"},
		{"two elements one name", `{items: [{sku: A}, {sku: A}]}`, "names two elements"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			mustCommit(t, s, nil, tc.seed)
			_, err := s.SetSchema(testSchema(t, `{define: {items: {sku: !logd-key null}}}`), false)
			if err == nil || !IsSchemaRefused(err) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("answered %v", err)
			}
			if schema, _ := s.GetActiveSchema(); schema != nil {
				t.Error("the refused schema became active")
			}
			if got := flatten(t, mustReadScope(t, s, 1, nil)); !strings.HasPrefix(got, "items: [") {
				t.Errorf("the data changed: %s", got)
			}
		})
	}
}

// Nested keyed arrays gaining identities together: the outer array is restated once,
// with the inner arrays in their new form inside it.
func TestSchemaRewrite_NestedArraysAreRestatedWithTheirContainer(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{orders: [{id: o1, lines: [{sku: A, n: 1}, {sku: B, n: 2}]}]}`)
	commit := migrateTo(t, s, `{define: {orders: {id: !logd-key null, lines: {sku: !logd-key null}}}}`)
	got := flatten(t, mustReadScope(t, s, commit, nil))
	if want := `orders: - id: o1 lines: - n: 1 sku: A - n: 2 sku: B`; got != want {
		t.Errorf("after the gain\n got %s\nwant %s", got, want)
	}
	c := mustCommit(t, s, nil, `{orders: [{id: o1, lines: [{sku: B, n: 20}]}]}`)
	if got := flatten(t, mustReadScope(t, s, c, nil)); got != `orders: - id: o1 lines: - n: 1 sku: A - n: 20 sku: B` {
		t.Errorf("a write by name into the inner array: %s", got)
	}
}

// The rewrite reaches a watcher as a delta in the client's vocabulary -- an array, keyed,
// as a total cover -- and survives a reopen and a rebuild from the log.
func TestSchemaRewrite_IsPublishedAndRebuilt(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	notified := make(chan *CommitNotification, 16)
	s.SetCommitNotifier(func(n *CommitNotification) { notified <- n })
	mustCommit(t, s, nil, `{items: [{sku: B}, {sku: A}]}`)
	commit := migrateTo(t, s, `{define: {items: {sku: !logd-key null}}}`)
	var got *CommitNotification
	for got == nil {
		select {
		case n := <-notified:
			if n.Commit == commit {
				got = n
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the schema commit's rewrite was not published")
		}
	}
	if got.Patch == nil {
		t.Fatal("the schema commit was published without its rewrite")
	}
	if d := flatten(t, got.Patch); d != `items: !key(sku).insert - { sku: A } - { sku: B }` {
		t.Errorf("the published delta: %s", d)
	}
	s.SetCommitNotifier(nil)

	check := func(when string, s *Storage) {
		t.Helper()
		if got := flatten(t, mustReadScope(t, s, commit, nil)); got != `items: - sku: A - sku: B` {
			t.Errorf("%s: %s", when, got)
		}
		if schema, at := s.GetActiveSchema(); schema == nil || at != commit {
			t.Errorf("%s: schema %v at %d, want at %d", when, schema != nil, at, commit)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err = Open(root, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	check("reopened", s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "index.manifest")); err != nil {
		t.Fatalf("remove index.manifest: %v", err)
	}
	s, err = Open(root, nil)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	defer s.Close()
	check("rebuilt from the log", s)
}

// An array holding nothing is not restated: the schema commit carries no delta.
func TestSchemaRewrite_NothingHeldNothingRestated(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: [], other: 1}`)
	commit := migrateTo(t, s, `{define: {items: {sku: !logd-key null}}}`)
	if got := flatten(t, mustReadScope(t, s, commit, nil)); got != `items: [] other: 1` {
		t.Errorf("after the gain: %s", got)
	}
	if patches := storedPatches(t, s); len(patches) != 1 {
		t.Errorf("the schema commit stored a delta: %v", patches)
	}
}
