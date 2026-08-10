package storage

import (
	"strings"
	"testing"
)

// !logd-key declares an array keyed on a field the CLIENT supplies. Until it existed,
// "items is keyed by name" was unsayable in schema: an array could only be called keyed as
// a side effect of calling its key auto-generated, which is why a client-supplied !key(name)
// had no schema route at all and had to ride on every write as a tag.

// TestSchemaKeyField_ClientSuppliedKeyIsSayable is the gap P1 opened on: a keyed array
// whose key the client provides, declared once, honoured by the index without the write
// carrying a tag.
func TestSchemaKeyField_ClientSuppliedKeyIsSayable(t *testing.T) {
	s := openTestStorage(t)
	node := mustParseBody(t, `{define: {items: {name: !logd-key null}}}`)
	if _, err := s.StartMigration(node); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	if _, err := s.CompleteMigration(); err != nil {
		t.Fatalf("CompleteMigration: %v", err)
	}

	if f := s.schemaForScope(nil).LookupKeyField("items"); f != "name" {
		t.Fatalf("LookupKeyField(items) = %q, want %q", f, "name")
	}
	// Auto-id must NOT be implied: the client supplies this key, nothing generates it.
	if aid := s.schemaForScope(nil).AutoID("items"); aid != nil {
		t.Errorf("!logd-key should not imply auto-id, got %+v", aid)
	}

	// A write carrying no !key tag is still indexed by identity.
	mustCommit(t, s, nil, `{items: [{name: "A", q: 1}, {name: "B", q: 2}]}`)
	paths := indexPathSet(s)
	t.Logf("index paths: %v", paths)
	for _, want := range []string{"items(A)", "items(B)"} {
		found := false
		for _, p := range paths {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Errorf("index has no %q; a declared key should not need a per-write tag\n got %v",
				want, paths)
		}
	}
}

// TestSchemaKeyField_AmbiguousSchemaIsRejected: key derivation decides what a stored delta
// records, and a delta cannot be un-recorded, so an ambiguous schema is refused where it is
// proposed rather than where it bites.
func TestSchemaKeyField_AmbiguousSchemaIsRejected(t *testing.T) {
	for _, tc := range []struct{ name, doc, wantErr string }{
		{
			name:    "two keys for one array",
			doc:     `{define: {items: {name: !logd-key null, other: !logd-key null}}}`,
			wantErr: "declared keyed by both",
		},
		{
			name:    "a key and an auto-id for one array",
			doc:     `{define: {items: {name: !logd-key null, id: !logd-auto-id null}}}`,
			wantErr: "one array has one identity",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			_, err := s.StartMigration(mustParseBody(t, tc.doc))
			if err == nil {
				t.Fatal("expected the migration to be refused")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error did not mention %q: %v", tc.wantErr, err)
			}
			t.Logf("  %v", err)
			// And nothing was adopted.
			if s.schemaForScope(nil) != nil {
				t.Error("a refused schema was adopted anyway")
			}
		})
	}
}
