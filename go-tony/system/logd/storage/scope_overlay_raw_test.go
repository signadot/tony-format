package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// commitRawAt writes src at path in scope, or baseline when scope is nil.
func commitRawAt(t *testing.T, s *Storage, scope *string, path, src string) int64 {
	t.Helper()
	tx, err := s.NewTx(1, scope)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	data, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	r := p.Commit()
	if !r.Committed {
		t.Fatalf("commit %q: %v", src, r.Error)
	}
	return r.Commit
}

// A charter rule as verse stores one: match operators under !raw, which is what
// says the subtree is data rather than instructions.
const rawRule = `{rules: !raw {outrun: {condition: {value: !let {let: [{tip: abc}], in: {state: open, base: !not .[tip]}}}}}}`

// TestScopeOverlay_RawWrappedOperatorsSurvive: a scope writing a document that
// holds operators as data must be readable afterwards.
//
// The overlay is built from materialized state, so every operator tag in it came
// from a document where it meant data. Storing one as an instruction makes the
// SCOPE unreadable, not just the entity: one unapplicable patch stops
// materialization, so a single write takes the store down for reads
// (issue 6225etzfh12kr955fxn0).
func TestScopeOverlay_RawWrappedOperatorsSurvive(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	scope := "sandbox1"
	commitRawAt(t, s, nil, "", `{rules: {other: {a: 1}}}`)
	c := commitRawAt(t, s, &scope, "", rawRule)

	if err := s.WriteScopeOverlay(scope, c); err != nil {
		t.Fatalf("WriteScopeOverlay: %v", err)
	}

	got, err := s.ReadStateAt("", c, &scope)
	if err != nil {
		t.Fatalf("scoped read after overlay: %v", err)
	}
	out := encode.MustString(got)
	// The tags are still there, as data: this is the document that was written.
	for _, want := range []string{"!let", "!not", "state: open"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stored rule lost %q:\n%s", want, out)
		}
	}

	// Baseline is untouched, and still readable, which is the part that made this
	// a store-wide outage rather than one bad entity.
	base, err := s.ReadStateAt("", c, nil)
	if err != nil {
		t.Fatalf("baseline read after overlay: %v", err)
	}
	if !strings.Contains(encode.MustString(base), "a: 1") {
		t.Errorf("baseline lost its own data:\n%s", encode.MustString(base))
	}
}

// TestScopeOverlay_OwnedPathValueIsEscaped: the overlay re-states each path the
// scope owns from the materialized scoped view, and that value is DATA. Putting
// it in a patch unescaped hands a stored rule to the patch applier as an
// instruction.
func TestScopeOverlay_OwnedPathValueIsEscaped(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	scope := "sandbox1"
	// Baseline and scope both write the same path, so the diff records nothing
	// for it and the owned-path re-statement is what carries it.
	commitRawAt(t, s, nil, "rules", `{value: {plain: 1}}`)
	c := commitRawAt(t, s, &scope, "rules", `!raw {value: !let {let: [{tip: abc}], in: {state: open}}}`)

	overlay, err := s.BuildScopeOverlay(scope, c)
	if err != nil {
		t.Fatalf("BuildScopeOverlay: %v", err)
	}
	if overlay != nil {
		if err := api.ValidateForStorage(overlay); err != nil {
			t.Fatalf("the overlay is not storable: %v\noverlay:\n%s", err, encode.MustString(overlay))
		}
	}
	if err := s.WriteScopeOverlay(scope, c); err != nil {
		t.Fatalf("WriteScopeOverlay: %v", err)
	}
	got, err := s.ReadStateAt("", c, &scope)
	if err != nil {
		t.Fatalf("scoped read after overlay: %v", err)
	}
	if out := encode.MustString(got); !strings.Contains(out, "!let") {
		t.Errorf("the scope's own value lost its tag:\n%s", out)
	}
}
