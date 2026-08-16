package storage

import (
	"testing"
)

// A scope keeps its own document and steps it, so a run of scoped writes does not
// rebuild the scoped view each time (sb33w8p9h12kr16kg5n0). What it must not do is
// answer with a document that is no longer current, and the whole guard is that
// commits are numbered globally: anything else committing takes the number in
// between and leaves the kept document a commit behind, where it is not used.
//
// These are the ways it could be wrong.
func TestScopeKeptDocumentStaysCurrent(t *testing.T) {
	scope := "s1"

	t.Run("a baseline write between two scoped writes is seen", func(t *testing.T) {
		s := openWithSeed(t, `{k: base1, other: 1}`)
		defer s.Close()

		if err := scopedCommit(t, s, &scope, "mine", `first`); err != nil {
			t.Fatalf("scoped: %v", err)
		}
		// Baseline moves a key the scope has NOT written.
		if _, err := arrayWriteCommit(t, s, "other", `2`); err != nil {
			t.Fatalf("baseline: %v", err)
		}
		// The scope writes again: its kept document is a commit behind now, so it
		// must be rebuilt rather than used.
		if err := scopedCommit(t, s, &scope, "mine", `second`); err != nil {
			t.Fatalf("scoped: %v", err)
		}

		if got, want := readScope(t, s, &scope), "k: base1 mine: second other: 2"; got != want {
			t.Errorf("got %s\nwant %s", got, want)
		}
	})

	t.Run("a scope's own write still shadows baseline", func(t *testing.T) {
		s := openWithSeed(t, `{k: base1}`)
		defer s.Close()

		// The scope owns k ...
		if err := scopedCommit(t, s, &scope, "k", `scoped1`); err != nil {
			t.Fatalf("scoped: %v", err)
		}
		// ... and baseline writes the same leaf. A scope's writes apply LAST, so the
		// scope keeps its value: this is the hazard that stops a scope folding a
		// baseline patch into its document at all (9b2vpggxh12ks0qde5n0).
		if _, err := arrayWriteCommit(t, s, "k", `base2`); err != nil {
			t.Fatalf("baseline: %v", err)
		}
		// A further scoped write must not resurrect base2 over the scope's value.
		if err := scopedCommit(t, s, &scope, "m", `1`); err != nil {
			t.Fatalf("scoped: %v", err)
		}

		if got, want := readScope(t, s, &scope), "k: scoped1 m: 1"; got != want {
			t.Errorf("the scope lost its own leaf:\n got %s\nwant %s", got, want)
		}
		if got, want := readScope(t, s, nil), "k: base2"; got != want {
			t.Errorf("baseline:\n got %s\nwant %s", got, want)
		}
	})

	t.Run("two scopes do not answer for each other", func(t *testing.T) {
		s := openWithSeed(t, `{k: base}`)
		defer s.Close()
		a, b := "a", "b"

		if err := scopedCommit(t, s, &a, "who", `a1`); err != nil {
			t.Fatalf("scope a: %v", err)
		}
		if err := scopedCommit(t, s, &b, "who", `b1`); err != nil {
			t.Fatalf("scope b: %v", err)
		}
		if err := scopedCommit(t, s, &a, "again", `a2`); err != nil {
			t.Fatalf("scope a: %v", err)
		}

		if got, want := readScope(t, s, &a), "again: a2 k: base who: a1"; got != want {
			t.Errorf("scope a:\n got %s\nwant %s", got, want)
		}
		if got, want := readScope(t, s, &b), "k: base who: b1"; got != want {
			t.Errorf("scope b:\n got %s\nwant %s", got, want)
		}
	})

	t.Run("a deleted scope keeps no document", func(t *testing.T) {
		s := openWithSeed(t, `{k: base}`)
		defer s.Close()

		if err := scopedCommit(t, s, &scope, "mine", `first`); err != nil {
			t.Fatalf("scoped: %v", err)
		}
		if err := s.DeleteScope(scope); err != nil {
			t.Fatalf("DeleteScope: %v", err)
		}
		s.commitMu.Lock()
		_, kept := s.scopeHeads[scope]
		s.commitMu.Unlock()
		if kept {
			t.Error("a document is held for a scope which no longer has any data")
		}
		if got, want := readScope(t, s, &scope), "k: base"; got != want {
			t.Errorf("after delete:\n got %s\nwant %s", got, want)
		}
	})
}

func openWithSeed(t *testing.T, seed string) *Storage {
	t.Helper()
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := arrayWriteCommit(t, s, "", seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return s
}

func readScope(t *testing.T, s *Storage, scope *string) string {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	doc, err := s.ReadStateAt("", commit, scope)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return flatten(t, doc)
}
