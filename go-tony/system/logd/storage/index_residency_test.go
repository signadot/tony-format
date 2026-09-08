package storage

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// A ceiling under the floor is refused rather than discovered as thrashing; zero is
// unbounded and the floor itself is allowed.
func TestAnIndexCeilingBelowTheFloorIsRefused(t *testing.T) {
	s := openTestStorage(t)
	if err := s.SetIndexCeiling(index.MinIndexCeiling - 1); err == nil {
		t.Errorf("a ceiling of %d, below the floor of %d, was accepted", index.MinIndexCeiling-1, index.MinIndexCeiling)
	}
	if err := s.SetIndexCeiling(-1); err == nil {
		t.Errorf("a negative ceiling was accepted")
	}
	if err := s.SetIndexCeiling(index.MinIndexCeiling); err != nil {
		t.Errorf("the floor itself was refused: %v", err)
	}
	if err := s.SetIndexCeiling(0); err != nil {
		t.Errorf("unbounded was refused: %v", err)
	}
	if got := s.index.Residency().Ceiling(); got != 0 {
		t.Errorf("ceiling after unbounded: %d", got)
	}
}

// EVICTION CHANGES A COST, NEVER AN ANSWER, at the store: the same stream of writes into a
// store whose index is held under the smallest ceiling there is, persisting as it goes so
// there is something to evict, and into one unbounded -- and every read at every path at
// every commit is the same bytes, with the ceiling having evicted and paged
// (index_residency.md; rebuild_plan.md phase 6).
func TestReadsUnderAnIndexCeilingAreReadsUnbounded(t *testing.T) {
	for seed := int64(1); seed <= 4; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			ops := genOps(rng, 300)
			ref := openTestStorage(t)
			subj := openTestStorage(t)
			if err := subj.SetIndexCeiling(index.MinIndexCeiling); err != nil {
				t.Fatalf("SetIndexCeiling: %v", err)
			}
			var commits []int64
			for i, o := range ops {
				rc, rerr := applyOp(t, ref, o)
				sc, serr := applyOp(t, subj, o)
				if (rerr == nil) != (serr == nil) || rc != sc {
					t.Fatalf("op %d %s: reference %d %v, subject %d %v", i, o, rc, rerr, sc, serr)
				}
				if rerr == nil {
					commits = append(commits, rc)
				}
				if o.snapshot {
					if err := subj.SwitchDLog(); err != nil {
						t.Fatalf("SwitchDLog: %v", err)
					}
				}
				if i%20 == 19 {
					if err := subj.index.Persist(subj.logGenerations()); err != nil {
						t.Fatalf("Persist: %v", err)
					}
				}
			}
			for _, commit := range commits[len(commits)/2:] {
				for _, kp := range readPaths {
					want := read(ref, kp, commit)
					got := read(subj, kp, commit)
					if !want.equal(got) {
						t.Fatalf("read(%q, commit=%d) under the ceiling differs\n  unbounded: %s\n  ceiling:   %s\n%s",
							kp, commit, want, got, dumpOps(ops))
					}
				}
			}
			st := subj.index.Residency().Stats()
			t.Logf("resident %d of ceiling %d, %d hits, %d misses, %d evictions, over %d",
				st.Resident, st.Ceiling, st.Hits, st.Misses, st.Evictions, st.Over)
			if st.Evictions == 0 || st.Misses == 0 {
				t.Errorf("the ceiling did nothing: %+v", st)
			}
		})
	}
}

// The store of the forensics' shape, served under a ceiling one tenth of what its index
// holds unbounded: every read answers what it answered before the ceiling, and the store
// says what it is holding and how often a read found its regions resident.
func TestTheShapedStoreServesUnderATenthOfItsIndex(t *testing.T) {
	s, paths := shapedStore(t, shape{paths: 240, writesPerPath: 4, snapshotEvery: 300,
		ancestors: []string{"verse.git.ref", "verse.github.issue", "verse.github.comment"}})
	head, _ := s.GetCurrentCommit()
	if err := s.index.Persist(s.logGenerations()); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	unbounded := s.index.Residency().Stats().Resident
	want := map[string]string{}
	for k := 0; k < len(paths); k += 7 {
		p := paths[k]
		got, _, err := readSubtreeAt(s, p, head, nil)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		want[p] = withComments(got)
	}
	root, _, err := readSubtreeAt(s, "", head, nil)
	if err != nil {
		t.Fatalf("root read: %v", err)
	}
	want[""] = withComments(root)

	ceiling := unbounded / 10
	if ceiling < index.MinIndexCeiling {
		ceiling = index.MinIndexCeiling
	}
	if err := s.SetIndexCeiling(ceiling); err != nil {
		t.Fatalf("SetIndexCeiling(%d): %v", ceiling, err)
	}
	for round := 0; round < 2; round++ {
		for p, w := range want {
			got, _, err := readSubtreeAt(s, p, head, nil)
			if err != nil {
				t.Fatalf("read %q under the ceiling: %v", p, err)
			}
			if g := withComments(got); g != w {
				t.Fatalf("read %q under the ceiling differs\n before %s\n after  %s", p, w, g)
			}
		}
	}
	st := s.index.Residency().Stats()
	rate := 0.0
	if st.Hits+st.Misses > 0 {
		rate = float64(st.Hits) / float64(st.Hits+st.Misses)
	}
	t.Logf("index unbounded %d bytes, ceiling %d, resident %d, hit rate %.2f (%d hits, %d misses), %d evictions, over %d",
		unbounded, ceiling, st.Resident, rate, st.Hits, st.Misses, st.Evictions, st.Over)
	if st.Evictions == 0 {
		t.Errorf("a ceiling of a tenth evicted nothing")
	}
	if st.Resident > ceiling+st.Over {
		t.Errorf("resident %d stands over the ceiling %d by more than the recorded excess %d", st.Resident, ceiling, st.Over)
	}
	if r, ok := s.StatsReport()["index.ceiling"]; !ok || r.(int64) != ceiling {
		t.Errorf("the report does not say the ceiling: %v", r)
	}
}
