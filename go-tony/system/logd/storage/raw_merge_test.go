package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The escape merges and !insert.raw replaces (mgg9nvt6h12krn6dksn0), seen through the
// store: a record put twice with a bare !raw reads as the union of its two versions,
// and put again with !insert.raw reads as exactly the last one. The second shape is
// what a consumer holding documents as data -- a charter, a goal -- writes, and it is
// what lets a log whose earlier puts were bare !raw be repaired by one re-put rather
// than a migration.
func TestRawMergesAndInsertRawReplaces(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{rules: {r1: !raw {a: 1, b: !glob "*"}}}`)
	commitAt(t, s, nil, "rules.r1", `!raw {a: 1, c: !irtype 0}`)
	expectAt(t, s, nil, "rules.r1", `{a: 1, b: !glob "*", c: !irtype 0}`)
	commitAt(t, s, nil, "rules.r1", `!insert.raw {a: 2, d: !all {}}`)
	expectAt(t, s, nil, "rules.r1", `{a: 2, d: !all {}}`)
}

// A scope's lowered claim is !insert.raw: the value is exactly this, as data, whatever
// baseline does under it afterwards (3xn08cb6h12kr4psg5n0 is the read; this is the shape).
func TestAScopesClaimIsInsertRaw(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"
	mustCommit(t, s, nil, `{a: {x: 1}}`)
	c := commitAt(t, s, &sc, "a", `!rename [{from: "x", to: "y"}]`)

	cur, err := s.Deltas(c, c, &sc, "")
	if err != nil {
		t.Fatalf("Deltas: %v", err)
	}
	defer cur.Close()
	n, err := cur.Next()
	if err != nil {
		t.Fatalf("the claim's delta: %v", err)
	}
	at := ir.Get(n.Patch, "a")
	if at == nil {
		t.Fatalf("no claim at a in %s", encode.MustString(n.Patch))
	}
	head, _, rest := ir.TagArgs(at.Tag)
	if head != "!insert" || !ir.TagHas(rest, "!raw") {
		t.Errorf("the claim is tagged %q, want !insert.raw", at.Tag)
	}

	commitAt(t, s, nil, "a.z", `5`)
	expectAt(t, s, &sc, "a", `{y: 1}`)
	expectAt(t, s, nil, "a", `{x: 1, z: 5}`)
}

// expectAt reads kp at the head in the given view and compares it to want as data.
func expectAt(t *testing.T, s *Storage, scope *string, kp, want string) {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	got, _, err := readSubtreeAt(s, kp, commit, scope)
	if err != nil {
		t.Fatalf("read %s: %v", kp, err)
	}
	w, err := parse.Parse([]byte(want))
	if err != nil {
		t.Fatalf("parse %q: %v", want, err)
	}
	if !mergeop.RawEqual(got, w) {
		t.Errorf("at %q:\n got  %s\n want %s", kp, encode.MustString(got), encode.MustString(w))
	}
}
