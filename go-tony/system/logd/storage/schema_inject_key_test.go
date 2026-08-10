package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

func declareKeyed(t *testing.T, s *Storage, doc string) {
	t.Helper()
	if _, err := s.StartMigration(mustParseBody(t, doc)); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	if _, err := s.CompleteMigration(); err != nil {
		t.Fatalf("CompleteMigration: %v", err)
	}
}

// TestInjectKeyTags_DeclaringAKeyChangesWhatAWriteMeans is the whole point: before
// injection, declaring a key changed how a write was INDEXED and not what it MEANT, so an
// untagged write merged positionally and replaced whatever sat at that position.
func TestInjectKeyTags_DeclaringAKeyChangesWhatAWriteMeans(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)

	mustCommit(t, s, nil, `{items: [{sku: "W", q: 1}, {sku: "X", q: 2}]}`)
	// No !key on this write. It names one element by its key; the others must survive.
	c := mustCommit(t, s, nil, `{items: [{sku: "G", q: 3}]}`)

	doc := mustReadScope(t, s, c, nil)
	got := skus(doc, "items")
	t.Logf("  state: %s", encodeWire(t, doc))
	if !sameSet(got, []string{"W", "X", "G"}) {
		t.Errorf("skus = %v, want {W,X,G}: an untagged write to a declared-keyed array must "+
			"merge by identity, not replace by position", got)
	}

	// Updating an existing key merges into that element rather than appending.
	c = mustCommit(t, s, nil, `{items: [{sku: "W", q: 99}]}`)
	doc = mustReadScope(t, s, c, nil)
	if got := skus(doc, "items"); !sameSet(got, []string{"W", "X", "G"}) {
		t.Errorf("after updating W: skus = %v, want {W,X,G} with no duplicate", got)
	}
	if q := intOf(elemField(t, doc, "items", "sku", "W", "q")); q != 99 {
		t.Errorf("W.q = %d, want 99", q)
	}
}

// TestInjectKeyTags_DisagreeingPatchIsRefused: two identities for one array is the
// ambiguity Schema.Validate refuses to adopt, and it is no better arriving one write at a
// time.
func TestInjectKeyTags_DisagreeingPatchIsRefused(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)

	data, err := parse.Parse([]byte(`{items: !key(name) [{name: "A", sku: "W"}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	txn, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	res := p.Commit()
	if res.Committed {
		t.Fatal("a patch keying items by name should be refused where the schema says sku")
	}
	if res.Error == nil || !strings.Contains(res.Error.Error(), "one array has one identity") {
		t.Errorf("unhelpful refusal: %v", res.Error)
	}
	t.Logf("  %v", res.Error)
}

// TestInjectKeyTags_AgreeingPatchIsLeftAlone: a client that repeats the schema's own
// keying is not fighting it.
func TestInjectKeyTags_AgreeingPatchIsLeftAlone(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)

	mustCommit(t, s, nil, `{items: [{sku: "W", q: 1}]}`)
	c := mustCommit(t, s, nil, `{items: !key(sku) [{sku: "G", q: 3}]}`)
	if got := skus(mustReadScope(t, s, c, nil), "items"); !sameSet(got, []string{"W", "G"}) {
		t.Errorf("skus = %v, want {W,G}", got)
	}
}
