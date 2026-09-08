package storage

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"testing"
	"time"
)

// scopeEntries counts the scope's entries the index holds.
func scopeEntries(s *Storage, scope string) int {
	n := 0
	for seg := range s.index.Segments("", nil, nil, &scope) {
		if seg.ScopeID != nil && *seg.ScopeID == scope && seg.StartCommit != seg.EndCommit {
			n++
		}
	}
	return n
}

// everythingBeyondCutoff is a compaction configuration under which every entry is past
// the cutoff.
func everythingBeyondCutoff() *CompactionConfig {
	return &CompactionConfig{
		Cutoff:       -time.Hour,
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  10 * time.Millisecond,
	}
}

// A scope that rewrote one field a hundred times carries one entry for it out of
// compaction, and reads the same before and after. What a later entry does not restate
// stays: a !delete nothing later covers, and a commented value that a later plain write at
// the same path replaces but might not have wiped the comment of (scope_compaction.go).
func TestAScopesDominatedEntriesGoBeyondTheCutoff(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"
	mustCommit(t, s, nil, `{a: {x: 1, y: 1}, keep: 0, arr: [1, 2]}`)
	for i := 1; i <= 100; i++ {
		commitAt(t, s, &sc, "a.x", fmt.Sprintf("%d", i))
	}
	commitAt(t, s, &sc, "a.y", `!delete`)
	commitAt(t, s, &sc, "keep", "# why\n7")
	commitAt(t, s, &sc, "keep", "8")
	commitAt(t, s, &sc, "arr", "[3, 4, 5]")
	commitAt(t, s, &sc, "arr", "[6]")
	head, _ := s.GetCurrentCommit()
	before, _, err := readSubtreeAt(s, "", head, &sc)
	if err != nil {
		t.Fatalf("scoped read: %v", err)
	}
	baseBefore, _, _ := readSubtreeAt(s, "", head, nil)

	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	if got := scopeEntries(s, sc); got != 105 {
		t.Fatalf("scope entries before compaction: %d, want 105", got)
	}
	if err := s.Compact(everythingBeyondCutoff()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// One a.x, the delete, the commented keep, the plain keep, one arr.
	if got := scopeEntries(s, sc); got != 5 {
		t.Errorf("scope entries after compaction: %d, want 5", got)
	}
	after, _, err := readSubtreeAt(s, "", head, &sc)
	if err != nil {
		t.Fatalf("scoped read after compaction: %v", err)
	}
	if withComments(after) != withComments(before) {
		t.Errorf("the scope reads differently after compaction\n before %s\n after  %s", withComments(before), withComments(after))
	}
	if baseAfter, _, _ := readSubtreeAt(s, "", head, nil); withComments(baseAfter) != withComments(baseBefore) {
		t.Errorf("baseline changed: %s -> %s", withComments(baseBefore), withComments(baseAfter))
	}
	if floor := s.ReplayFloor(); floor == 0 {
		t.Errorf("dropping the scope's entries left the replay floor at 0")
	}
}

// Two stores take the same mixed stream of baseline and scoped writes; one compacts
// both of its logs with everything beyond the cutoff, the other never does. Every read
// at the head, in both views, agrees -- and a scoped replay from above the floor delivers
// what the uncompacted store delivers.
func TestScopeCompactionDifferential(t *testing.T) {
	const scope = "s1"
	paths := []string{"", "a", "a.b", "a.b.c", "d", "d.e", "k0"}
	for seed := 1; seed <= seedCount(); seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(seed)))
			ops := genScopeOps(rng, 60)
			ref := openTestStorage(t)
			subj := openTestStorage(t)
			var commits []int64
			for i, o := range ops {
				rc, rerr := applyScopeOp(t, ref, o, scope)
				sc, serr := applyScopeOp(t, subj, o, scope)
				if (rerr == nil) != (serr == nil) || rc != sc {
					t.Fatalf("op %d %s: reference %d %v, subject %d %v", i, o, rc, rerr, sc, serr)
				}
				if rerr == nil {
					commits = append(commits, rc)
				}
				if o.snapshot {
					if err := subj.SwitchDLog(); err != nil {
						t.Fatalf("op %d: SwitchDLog: %v", i, err)
					}
				}
			}
			for range 2 { // both logs, each once inactive
				if err := subj.SwitchDLog(); err != nil {
					t.Fatalf("SwitchDLog: %v", err)
				}
				if err := subj.Compact(everythingBeyondCutoff()); err != nil {
					t.Fatalf("Compact: %v", err)
				}
			}
			head := commits[len(commits)-1]
			sc := scope
			for _, view := range []*string{nil, &sc} {
				for _, kp := range paths {
					want, _, err := readSubtreeAt(ref, kp, head, view)
					if err != nil {
						t.Fatalf("reference read %q: %v", kp, err)
					}
					got, _, err := readSubtreeAt(subj, kp, head, view)
					if err != nil {
						t.Fatalf("subject read %q: %v", kp, err)
					}
					if withComments(got) != withComments(want) {
						t.Fatalf("read %q (scope %v) differs after compaction\n reference %s\n subject   %s\n%s",
							kp, view != nil, withComments(want), withComments(got), dumpScopeOps(ops))
					}
				}
			}
			floor := subj.ReplayFloor()
			if floor == 0 {
				return // nothing was dropped on this stream
			}
			if _, err := subj.Deltas(floor, head, &sc, ""); !errors.Is(err, ErrReplayCompacted) {
				t.Errorf("a scoped replay from the floor was not refused: %v", err)
			}
			want := replayCommits(t, ref, floor+1, head, &sc)
			got := replayCommits(t, subj, floor+1, head, &sc)
			if !slices.Equal(got, want) {
				t.Errorf("a scoped replay from above the floor delivers %v, the uncompacted store %v", got, want)
			}
		})
	}
}

func replayCommits(t *testing.T, s *Storage, from, to int64, scope *string) []int64 {
	t.Helper()
	cur, err := s.Deltas(from, to, scope, "")
	if err != nil {
		t.Fatalf("Deltas(%d, %d): %v", from, to, err)
	}
	defer cur.Close()
	var out []int64
	for {
		n, err := cur.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("Deltas: %v", err)
		}
		out = append(out, n.Commit)
	}
}

func dumpScopeOps(ops []scopeOp) string {
	out := "  write stream:\n"
	for i, o := range ops {
		out += fmt.Sprintf("    %2d: %s\n", i+1, o)
	}
	return out
}
