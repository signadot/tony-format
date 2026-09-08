package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A read that folds a long tail at a path leaves a snapshot of that path behind it, and
// the next read there folds only what came after (path_snapshot.go). The snapshot is of
// the path: it sits at a.b in the index, at the commit the read was at.
func TestAReadWithALongTailTakesASnapshotOfItsPath(t *testing.T) {
	s := openTestStorage(t)
	// Off while the tail is built: a write's own read of its site is a read, and would
	// otherwise snapshot the path partway through.
	s.SetPathSnapshotPolicy(-1, 0)
	mustCommit(t, s, nil, `{a: {b: 0, c: 0}}`)
	for i := 1; i <= 20; i++ {
		commitAt(t, s, nil, "a.b", fmt.Sprintf("%d", i))
	}
	head, _ := s.GetCurrentCommit()
	s.SetPathSnapshotPolicy(8, 0)

	got, _, err := readSubtreeAt(s, "a.b", head, nil)
	if err != nil || got == nil || got.Int64 == nil || *got.Int64 != 20 {
		t.Fatalf("read a.b: %v (%v)", got, err)
	}
	s.waitPathSnapshots()
	st := s.ReadStats()
	if st.PathSnapshots != 1 {
		t.Fatalf("path snapshots taken: %d, want 1", st.PathSnapshots)
	}
	if st.LongestTail != 21 {
		t.Errorf("longest tail: %d, want the 21 writes that reached a.b -- the document and the twenty", st.LongestTail)
	}
	seg, ok := s.index.SnapshotAtOrAbove("a.b", head)
	if !ok || seg.KindedPath != "a.b" || seg.StartCommit != head {
		t.Fatalf("the seek at a.b finds %+v (%v); want a snapshot of a.b at %d", seg, ok, head)
	}

	// The next read seeks the path's snapshot and folds nothing.
	before := s.ReadStats()
	if got, _, err = readSubtreeAt(s, "a.b", head, nil); err != nil || *got.Int64 != 20 {
		t.Fatalf("read a.b again: %v (%v)", got, err)
	}
	after := s.ReadStats()
	if after.SeekPath != before.SeekPath+1 {
		t.Errorf("the second read did not seek the path's snapshot: %+v", after)
	}
	if after.Folded != before.Folded {
		t.Errorf("the second read folded %d records after the snapshot, want 0", after.Folded-before.Folded)
	}

	// Five more writes: the read folds the five, and a tail under the threshold
	// schedules nothing.
	s.SetPathSnapshotPolicy(-1, 0)
	for i := 21; i <= 25; i++ {
		commitAt(t, s, nil, "a.b", fmt.Sprintf("%d", i))
	}
	head, _ = s.GetCurrentCommit()
	s.SetPathSnapshotPolicy(8, 0)
	before = s.ReadStats()
	if got, _, err = readSubtreeAt(s, "a.b", head, nil); err != nil || *got.Int64 != 25 {
		t.Fatalf("read a.b after five more: %v (%v)", got, err)
	}
	s.waitPathSnapshots()
	after = s.ReadStats()
	if after.Folded-before.Folded != 5 {
		t.Errorf("the third read folded %d records, want 5", after.Folded-before.Folded)
	}
	if after.PathSnapshots != 1 {
		t.Errorf("a tail of 5 under a threshold of 8 took a snapshot: %d taken", after.PathSnapshots)
	}
	// And the field beside it is untouched by any of this.
	if c, _, err := readSubtreeAt(s, "a.c", head, nil); err != nil || c == nil || *c.Int64 != 0 {
		t.Errorf("a.c: %v (%v)", c, err)
	}
}

// A snapshot of a.b is the base for a read at a.b and below it, opened at the read's path
// inside it; a read above a.b does not see it and folds the writes instead. Either way the
// answer is the answer a store with no snapshot gives, comments counted.
func TestASnapshotOfAPathServesReadsBelowItAndNotAbove(t *testing.T) {
	s := openTestStorage(t)
	s.SetPathSnapshotPolicy(-1, 0)
	ref := openTestStorage(t)
	ref.SetPathSnapshotPolicy(-1, 0)
	both := func(path, src string) {
		commitAt(t, s, nil, path, src)
		commitAt(t, ref, nil, path, src)
	}
	both("", "{a: {b: {c: 1, d: {e: 2}}, x: 3}, f: 4}")
	both("a.b.c", "10")
	both("a.b.d.e", "# why\n20")
	both("a.x", "30")
	both("f", "40")
	head, _ := s.GetCurrentCommit()
	if err := s.snapshotPath(head, "a.b"); err != nil {
		t.Fatalf("snapshotPath: %v", err)
	}
	if seg, ok := s.index.SnapshotAtOrAbove("a.b.c", head); !ok || seg.KindedPath != "a.b" {
		t.Fatalf("the seek at a.b.c finds %+v (%v); want the snapshot of a.b", seg, ok)
	}
	both("a.b.c", "11")
	both("f", "41")
	both("a.b", "{g: 5}")
	head, _ = s.GetCurrentCommit()

	for _, kp := range []string{"a.b.c", "a.b.d.e", "a.b.d", "a.b", "a.b.g", "a", "a.x", "f", ""} {
		before := s.ReadStats()
		got, _, err := readSubtreeAt(s, kp, head, nil)
		if err != nil {
			t.Fatalf("read %q: %v", kp, err)
		}
		want, _, err := readSubtreeAt(ref, kp, head, nil)
		if err != nil {
			t.Fatalf("reference read %q: %v", kp, err)
		}
		if withComments(got) != withComments(want) {
			t.Errorf("read %q through the snapshot of a.b: %s\n           without: %s", kp, withComments(got), withComments(want))
		}
		after := s.ReadStats()
		under := kp == "a.b" || len(kp) > 3 && kp[:4] == "a.b."
		if hit := after.SeekPath - before.SeekPath; under && hit != 1 {
			t.Errorf("read %q under a.b did not seek its snapshot", kp)
		} else if !under && hit != 0 {
			t.Errorf("read %q above a.b seeks a snapshot it cannot use", kp)
		}
	}
}

// The snapshot is a log record like any other: the reopened store finds it in the index it
// saved, and a store whose index is rebuilt from the log finds it at its path again.
func TestASnapshotOfAPathSurvivesReopenAndRebuild(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s.SetPathSnapshotPolicy(-1, 0)
	mustCommit(t, s, nil, `{a: {b: {c: 1}}, f: 2}`)
	for i := 2; i <= 6; i++ {
		commitAt(t, s, nil, "a.b.c", fmt.Sprintf("%d", i))
	}
	head, _ := s.GetCurrentCommit()
	if err := s.snapshotPath(head, "a.b"); err != nil {
		t.Fatalf("snapshotPath: %v", err)
	}
	want, _, err := readSubtreeAt(s, "a.b", head, nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	for _, step := range []string{"reopened", "rebuilt"} {
		if step == "rebuilt" {
			if err := os.Remove(filepath.Join(dir, "index.manifest")); err != nil {
				t.Fatalf("remove index: %v", err)
			}
		}
		s, err = Open(dir, nil)
		if err != nil {
			t.Fatalf("%s: Open: %v", step, err)
		}
		s.SetPathSnapshotPolicy(-1, 0)
		seg, ok := s.index.SnapshotAtOrAbove("a.b.c", head)
		if !ok || seg.KindedPath != "a.b" || seg.StartCommit != head {
			t.Fatalf("%s: the seek at a.b.c finds %+v (%v); want the snapshot of a.b at %d", step, seg, ok, head)
		}
		before := s.ReadStats()
		got, _, err := readSubtreeAt(s, "a.b", head, nil)
		if err != nil {
			t.Fatalf("%s: read: %v", step, err)
		}
		if withComments(got) != withComments(want) {
			t.Errorf("%s: read a.b = %s, want %s", step, withComments(got), withComments(want))
		}
		if after := s.ReadStats(); after.SeekPath != before.SeekPath+1 {
			t.Errorf("%s: the read did not seek the path's snapshot", step)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("%s: Close: %v", step, err)
		}
	}
}

// Retention treats a snapshot of a path as it treats the writes it stands in for: kept
// within the cutoff, dropped beyond it -- and it takes no tier slot from the root
// snapshot, which is still there to fall back to.
func TestASnapshotOfAPathIsKeptWithinTheCutoffAndDroppedBeyondIt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cutoff time.Duration
		want   string // the path of the snapshot the seek at a.b lands on afterwards
	}{
		{"within the cutoff", time.Hour, "a.b"},
		{"beyond the cutoff", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			s.SetPathSnapshotPolicy(-1, 0)
			mustCommit(t, s, nil, `{a: {b: 1}}`)
			if err := s.SwitchDLog(); err != nil {
				t.Fatalf("SwitchDLog: %v", err)
			}
			for i := 2; i <= 6; i++ {
				commitAt(t, s, nil, "a.b", fmt.Sprintf("%d", i))
			}
			head, _ := s.GetCurrentCommit()
			if err := s.snapshotPath(head, "a.b"); err != nil {
				t.Fatalf("snapshotPath: %v", err)
			}
			cfg := &CompactionConfig{
				Cutoff:       tc.cutoff,
				BaseInterval: time.Hour,
				SlotsPerTier: 8,
				Multiplier:   2,
				GracePeriod:  100 * time.Millisecond,
			}
			if err := s.Compact(cfg); err != nil {
				t.Fatalf("Compact: %v", err)
			}
			seg, ok := s.index.SnapshotAtOrAbove("a.b", head)
			if !ok || seg.KindedPath != tc.want {
				t.Fatalf("after compaction the seek at a.b finds %+v (%v); want a snapshot of %q", seg, ok, tc.want)
			}
			if got, _, err := readSubtreeAt(s, "a.b", head, nil); err != nil || got == nil || *got.Int64 != 6 {
				t.Errorf("read a.b after compaction: %v (%v)", got, err)
			}
		})
	}
}

// The snapshot declines rather than fails when there is nothing worth taking: an absent
// path, a read that did not finish, a commit before the root snapshot in the log it would
// be written to, and a subtree over the budget -- after which the log is still walkable.
func TestASnapshotOfAPathDeclinesWhatIsNotWorthTaking(t *testing.T) {
	s := openTestStorage(t)
	s.SetPathSnapshotPolicy(-1, 0)
	mustCommit(t, s, nil, `{a: {b: 1, big: {p: "0123456789abcdef", q: "0123456789abcdef"}}}`)
	for i := 2; i <= 6; i++ {
		commitAt(t, s, nil, "a.b", fmt.Sprintf("%d", i))
	}
	if err := s.SwitchDLog(); err != nil { // the root snapshot, at commit 6
		t.Fatalf("SwitchDLog: %v", err)
	}
	head, _ := s.GetCurrentCommit()

	// Absent.
	if err := s.snapshotPath(head, "a.nope"); err != nil {
		t.Fatalf("snapshotPath of an absent path: %v", err)
	}
	if seg, ok := s.index.SnapshotAtOrAbove("a.nope", head); ok && seg.KindedPath == "a.nope" {
		t.Errorf("a snapshot of an absent path was indexed")
	}
	// A commit before the root snapshot.
	if err := s.snapshotPath(3, "a.b"); err != nil {
		t.Fatalf("snapshotPath at an old commit: %v", err)
	}
	if seg, _ := s.index.SnapshotAtOrAbove("a.b", head); seg.KindedPath != "" {
		t.Errorf("a snapshot at commit 3 was written behind the root snapshot at %d", seg.StartCommit)
	}
	// A read that did not finish: a long tail, Presence, Close.
	for i := 7; i <= 12; i++ {
		commitAt(t, s, nil, "a.b", fmt.Sprintf("%d", i))
	}
	head, _ = s.GetCurrentCommit()
	s.SetPathSnapshotPolicy(2, 0)
	c, err := s.Read(head, nil, "a.b")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	c.Presence()
	c.Close()
	s.waitPathSnapshots()
	if st := s.ReadStats(); st.PathSnapshots != 0 {
		t.Errorf("an unfinished read took a snapshot")
	}
	// Over the budget: the read's bytes exceed it, so nothing is scheduled -- not by
	// the writes' own reads of the site either; and a snapshot forced by hand abandons
	// its blob and leaves the log walkable.
	s.SetPathSnapshotPolicy(2, 8)
	for i := 1; i <= 4; i++ {
		commitAt(t, s, nil, "a.big.p", fmt.Sprintf(`"%d123456789abcdef"`, i))
	}
	head, _ = s.GetCurrentCommit()
	if _, _, err := readSubtreeAt(s, "a.big", head, nil); err != nil {
		t.Fatalf("read a.big: %v", err)
	}
	s.waitPathSnapshots()
	if err := s.snapshotPath(head, "a.big"); err != nil {
		t.Fatalf("snapshotPath over budget: %v", err)
	}
	if st := s.ReadStats(); st.PathSnapshots != 0 {
		t.Errorf("a subtree over the budget was snapshotted")
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog after an abandoned snapshot: %v", err)
	}
	if got, _, err := readSubtreeAt(s, "a.b", head, nil); err != nil || got == nil || *got.Int64 != 12 {
		t.Errorf("read a.b after an abandoned snapshot: %v (%v)", got, err)
	}
}

// At the shape of the forensics: a hot path under a shallow ancestor, written on most
// commits, no root snapshot in sight. Once a read there has taken its snapshot, no read
// that follows folds more than the policy's tail plus one round of writes -- the tail
// grows until it passes the threshold and the next read starts it over -- while the store
// keeps writing around it. The interval the switch would have set never enters into it.
func TestAHotShallowPathFoldsAtMostTheTail(t *testing.T) {
	s, paths := shapedStore(t, shape{paths: 60, writesPerPath: 3, snapshotEvery: 0,
		ancestors: []string{"verse.git.ref", "verse.github.issue"}})
	const hot = "verse.git.ref.hot"
	s.SetPathSnapshotPolicy(-1, 0)
	for i := 0; i < 200; i++ {
		if err := scopedCommit(t, s, nil, hot, fmt.Sprintf("{seq: %d}", i)); err != nil {
			t.Fatalf("write hot: %v", err)
		}
	}
	s.SetPathSnapshotPolicy(16, 0)
	head, _ := s.GetCurrentCommit()
	if _, _, err := readSubtreeAt(s, hot, head, nil); err != nil {
		t.Fatalf("read hot: %v", err)
	}
	s.waitPathSnapshots()
	if st := s.ReadStats(); st.PathSnapshots != 1 || st.LongestTail < 200 {
		t.Fatalf("after the first read: %d snapshots, longest tail %d", st.PathSnapshots, st.LongestTail)
	}

	for round := 1; round <= 5; round++ {
		s.SetPathSnapshotPolicy(-1, 0)
		for i := 0; i < 8; i++ {
			if err := scopedCommit(t, s, nil, hot, fmt.Sprintf("{seq: %d}", round*1000+i)); err != nil {
				t.Fatalf("write hot: %v", err)
			}
			if err := scopedCommit(t, s, nil, paths[i], fmt.Sprintf("{v: %d}", round)); err != nil {
				t.Fatalf("write %s: %v", paths[i], err)
			}
		}
		s.SetPathSnapshotPolicy(16, 0)
		head, _ = s.GetCurrentCommit()
		before := s.ReadStats()
		got, _, err := readSubtreeAt(s, hot, head, nil)
		if err != nil {
			t.Fatalf("round %d: read hot: %v", round, err)
		}
		after := s.ReadStats() // the read's own numbers, before the snapshot it may have scheduled reads too
		s.waitPathSnapshots()
		if seq := ir.Get(got, "seq"); seq == nil || seq.Int64 == nil || *seq.Int64 != int64(round*1000+7) {
			t.Errorf("round %d: hot = %s", round, withComments(got))
		}
		if folded := after.Folded - before.Folded; folded > 16+8 {
			t.Errorf("round %d: the read folded %d records since the snapshot, more than the tail and one round", round, folded)
		}
		if after.SeekPath != before.SeekPath+1 {
			t.Errorf("round %d: the read did not seek the path's snapshot", round)
		}
	}
	if st := s.ReadStats(); st.PathSnapshots < 2 {
		t.Errorf("the snapshot was never renewed: %d taken over five rounds of eight writes", st.PathSnapshots)
	}
}
