package storage

import "testing"

// !raw is how a document which CONTAINS operators is stored -- a charter, a rule, a
// schema -- and it has to survive being read back, whatever the base looks like.
//
// It did not. The streaming processor combined the patches pending under a key by
// APPLYING them to null, and applying a !raw patch consumes the !raw: that is its
// whole contract, install the subtree as data with its tags intact. The combined
// node therefore came back with the operators bare, the caller applied it as a
// patch, and they were live again:
//
//	failed to apply patches: irtype patching "null" gave cannot patch with irtype
//	operation
//
// Every read of the store failed from then on, so the escape the store documents
// as the way to hold such a document is what broke it. The trigger is a base that
// does not already have the key -- a snapshot taken before the write -- which is
// the path where the patches are combined rather than applied to something.
func TestRawSurvivesEveryBase(t *testing.T) {
	const spec = `!raw {answer: {files: !irtype 0, findings: !all {symbol: !irtype x}, ` +
		`branch: !glob "auto/*"}}`

	for _, tc := range []struct {
		name   string
		before func(t *testing.T, s *Storage)
	}{
		{
			name:   "no snapshot, key absent",
			before: func(t *testing.T, s *Storage) {},
		},
		{
			name: "snapshot taken before the write",
			before: func(t *testing.T, s *Storage) {
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("snapshot: %v", err)
				}
			},
		},
		{
			name: "snapshot, then the key deleted, then written",
			before: func(t *testing.T, s *Storage) {
				if _, err := arrayWriteCommit(t, s, "rules.r1", `1`); err != nil {
					t.Fatalf("seed rule: %v", err)
				}
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("snapshot: %v", err)
				}
				if _, err := arrayWriteCommit(t, s, "rules.r1", `!delete 1`); err != nil {
					t.Fatalf("delete: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			if _, err := arrayWriteCommit(t, s, "", `{rules: {}}`); err != nil {
				t.Fatalf("seed: %v", err)
			}
			tc.before(t, s)
			if _, err := arrayWriteCommit(t, s, "rules.r1", spec); err != nil {
				t.Fatalf("write the escaped spec: %v", err)
			}

			commit, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatal(err)
			}
			doc, err := s.ReadStateAt("", commit, nil)
			if err != nil {
				t.Fatalf("the store cannot be read: %v", err)
			}
			// and what came back is the document, with its operators as DATA
			got := flatten(t, doc)
			for _, want := range []string{"!irtype", "!all", "!glob"} {
				if !contains(got, want) {
					t.Errorf("the escaped document lost %s: %s", want, got)
				}
			}
			if contains(got, "!raw") {
				t.Errorf("the escape itself was stored: %s", got)
			}
		})
	}
}

func contains(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
