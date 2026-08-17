package storage

import (
	"strconv"
	"strings"
	"testing"
)

// A narrow read seeks to the snapshot index entry at or before the path it wants, and
// builds its stream state from THAT path -- so a store holding one path whose field
// name needs quoting broke reads which never mentioned it. Staging held
// `verse.github.pr."signadot/signadot#7349".checks`; a read of
// `verse.github.run.signadot` seeked near it and took logd down with it
// (0w79k6hqh12krgcwgdn0).
//
// The names here are the ones a github mirror actually writes: a repo path with a
// slash and an issue number with a hash.
func TestNarrowReadOverQuotedSnapshotPaths(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer s.Close()

	blob := strings.Repeat("x", 200)
	// enough entries, with enough bulk, that the snapshot index has entries between
	// the quoted paths and the path read below
	for i := 0; i < 200; i++ {
		id := "signadot/signadot#" + strconv.Itoa(7000+i)
		subtreeWrite(t, s, `verse.github.pr."`+id+`"`, "{number: "+strconv.Itoa(7000+i)+", blob: "+blob+"}")
		subtreeWrite(t, s, `verse.github.pr."`+id+`".checks`, "{state: green, blob: "+blob+"}")
	}
	subtreeWrite(t, s, "verse.github.run.signadot", "{id: run-1, state: done}")
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("snapshot: %s", err)
	}
	subtreeWrite(t, s, "verse.github.run.signadot", "{id: run-1, state: rerun}")

	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("commit: %s", err)
	}

	full, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("wide read: %s", err)
	}

	for _, path := range []string{
		"verse.github.run.signadot",                       // the read staging made
		`verse.github.pr."signadot/signadot#7100".checks`, // and one at a quoted path
		`verse.github.pr."signadot/signadot#7100"`,        // and at its parent
	} {
		t.Run(path, func(t *testing.T) {
			want, err := full.GetKPath(path)
			if err != nil {
				t.Fatalf("navigate %q: %s", path, err)
			}
			got, narrowed, err := s.ReadSubtreeAt(path, commit, nil)
			if err != nil {
				t.Fatalf("narrow read: %s", err)
			}
			if !narrowed {
				t.Fatalf("declined to narrow, so the quoted path was never seeked over")
			}
			if !got.DeepEqual(want) {
				t.Errorf("narrow read at %q differs from the wide one\n narrow %s\n wide   %s",
					path, mustEncode(t, got), mustEncode(t, want))
			}
		})
	}
}
