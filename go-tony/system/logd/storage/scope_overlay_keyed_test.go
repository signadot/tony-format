package storage

import "testing"

// The guard lifts once the schema declares the keying: annotation reproduces !key on both
// states so the diff keys itself, ownership is per ELEMENT rather than per array, and the
// overlay leaves alone every element baseline owns.
//
// Before, all of that failed silently -- the scope froze baseline's whole array.
func TestScopeOverlay_KeyedUnderSchema(t *testing.T) {
	for _, tc := range []struct{ name, base2 string }{
		{"baseline adds an element", `{items: [{sku: "S", q: 1}]}`},
		{"baseline updates its own element", `{items: [{sku: "W", q: 9}]}`},
		{"baseline removes its own element", `{items: [{sku: "W", q: 1}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			scope := "s1"
			if _, err := s.StartMigration(mustParseBody(t, `{define: {items: {sku: !logd-key null}}}`)); err != nil {
				t.Fatalf("StartMigration: %v", err)
			}
			if _, err := s.CompleteMigration(); err != nil {
				t.Fatalf("CompleteMigration: %v", err)
			}

			mustCommit(t, s, nil, `{items: [{sku: "W", q: 1}]}`)
			mustCommit(t, s, &scope, `{items: [{sku: "G", q: 3}]}`)

			if s.scopeHasKeyedPaths(scope) {
				t.Fatal("a schema-declared keyed path should no longer force replay")
			}
			commit, _ := s.GetCurrentCommit()
			ov, err := s.BuildScopeOverlay(scope, commit)
			if err != nil {
				t.Fatalf("BuildScopeOverlay: %v", err)
			}
			t.Logf("  overlay: %s", nodeOrNil(t, ov))
			if err := s.WriteScopeOverlay(scope, commit); err != nil {
				t.Fatalf("WriteScopeOverlay: %v", err)
			}

			mustCommit(t, s, nil, tc.base2)
			c2, _ := s.GetCurrentCommit()
			got, want := readBoth(t, s, c2, scope)
			t.Logf("  overlay read: %s", got)
			t.Logf("  replay  read: %s", want)
			if got != want {
				t.Errorf("overlay and replay disagree\n overlay: %s\n replay:  %s", got, want)
			}
		})
	}
}
