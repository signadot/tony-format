package storage

import "testing"

// A scope holding a keyed array must fall back to replay rather than answer from an
// overlay that cannot represent it. Before this guard both of these were silently wrong:
// the scope froze baseline's whole array, losing elements baseline added and updates
// baseline made to its own.
func TestScopeOverlay_KeyedFallsBackToReplay(t *testing.T) {
	for _, tc := range []struct{ name, base2 string }{
		{"baseline adds an element", `{items: !key(sku) [{sku: "S", q: 1}]}`},
		{"baseline updates its own element", `{items: !key(sku) [{sku: "W", q: 9}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			scope := "s1"
			mustCommit(t, s, nil, `{items: !key(sku) [{sku: "W", q: 1}]}`)
			mustCommit(t, s, &scope, `{items: !key(sku) [{sku: "G", q: 3}]}`)

			if !s.scopeHasKeyedPaths(scope) {
				t.Fatal("expected the scope to be detected as holding keyed paths")
			}
			if err := s.SwitchDLog(); err != nil { // would write an overlay if allowed
				t.Fatalf("SwitchDLog: %v", err)
			}
			if n := countOverlays(s, scope); n != 0 {
				t.Errorf("wrote %d overlays for a keyed scope, want 0", n)
			}

			mustCommit(t, s, nil, tc.base2)
			c2, _ := s.GetCurrentCommit()
			got, want := readBoth(t, s, c2, scope)
			if got != want {
				t.Errorf("keyed scope answered differently with the overlay on\n on:  %s\n off: %s", got, want)
			}
			t.Logf("  %s", got)
		})
	}
}
