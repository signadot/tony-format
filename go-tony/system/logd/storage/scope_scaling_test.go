package storage

import (
	"fmt"
	"testing"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Scaling measurements for scoped I/O. These are not assertions about absolute
// latency; they exist to show how each path grows with the number of writes, so a
// claim like "a scoped read is linear in the scope's write count" can be checked
// against numbers instead of reasoning.
//
// Every write here targets the SAME two keys, so the document stays a constant size
// and the only thing varying is how many patch entries the read has to replay.

func scalingCommit(t *testing.T, s *Storage, scope *string, body string, match *api.PathData) int64 {
	t.Helper()
	data, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse(%q): %v", body, err)
	}
	txn, err := s.NewTx(1, scope)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	p, err := txn.NewPatcher(&api.Patch{Match: match, PathData: api.PathData{Path: "", Data: data}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	res := p.Commit()
	if !res.Committed {
		t.Fatalf("commit not committed (matched=%v): %v", res.Matched, res.Error)
	}
	return res.Commit
}

func matchTag(t *testing.T) *api.PathData {
	t.Helper()
	pat, err := parse.Parse([]byte(`"t"`))
	if err != nil {
		t.Fatalf("parse match: %v", err)
	}
	return &api.PathData{Path: "tag", Data: pat}
}

// timeN runs f n times and returns the mean duration.
func timeN(n int, f func()) time.Duration {
	start := time.Now()
	for i := 0; i < n; i++ {
		f()
	}
	return time.Since(start) / time.Duration(n)
}

// setupStore writes baselineWrites baseline patches and scopeWrites scope patches,
// then takes a baseline snapshot (SwitchDLog) so the baseline read path starts from a
// snapshot — the steady state a long-lived store is actually in.
func setupStore(t *testing.T, baselineWrites, scopeWrites int, scope *string) (*Storage, int64) {
	t.Helper()
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < baselineWrites; i++ {
		scalingCommit(t, s, nil, fmt.Sprintf(`{ctr: %d, tag: "t"}`, i), nil)
	}
	for i := 0; i < scopeWrites; i++ {
		scalingCommit(t, s, scope, fmt.Sprintf(`{ctr: %d, tag: "t"}`, i), nil)
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	return s, commit
}

// TestScaling_Reads measures read latency as the number of writes grows, for four
// combinations: baseline reads over baseline writes, scoped reads over scope writes,
// and each with the other side of the store held at one write.
func TestScaling_Reads(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	scope := "sandbox"
	sizes := []int{50, 100, 200, 400}
	const reps = 20

	t.Log("baseline read, N baseline writes (snapshot taken at N):")
	for _, n := range sizes {
		s, commit := setupStore(t, n, 0, &scope)
		d := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, nil); err != nil {
				t.Fatalf("read: %v", err)
			}
		})
		t.Logf("  N=%4d  %v", n, d)
		s.Close()
	}

	t.Log("scoped read, N scope writes (1 baseline write, snapshot taken at N+1):")
	for _, n := range sizes {
		s, commit := setupStore(t, 1, n, &scope)
		d := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, &scope); err != nil {
				t.Fatalf("read: %v", err)
			}
		})
		t.Logf("  N=%4d  %v", n, d)
		s.Close()
	}

	t.Log("scoped read, 1 scope write, N BASELINE writes (snapshot taken at N+1):")
	for _, n := range sizes {
		s, commit := setupStore(t, n, 1, &scope)
		d := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, &scope); err != nil {
				t.Fatalf("read: %v", err)
			}
		})
		t.Logf("  N=%4d  %v", n, d)
		s.Close()
	}
}

// TestScaling_Writes measures commit latency at a given accumulated write count, for
// unconditional and conditional (CAS) writes, baseline and scoped.
func TestScaling_Writes(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	scope := "sandbox"
	sizes := []int{50, 100, 200, 400}
	const reps = 20

	for _, conditional := range []bool{false, true} {
		label := "unconditional"
		if conditional {
			label = "conditional (CAS)"
		}

		t.Logf("%s BASELINE write, after N baseline writes:", label)
		for _, n := range sizes {
			s, _ := setupStore(t, n, 0, &scope)
			var m *api.PathData
			if conditional {
				m = matchTag(t)
			}
			d := timeN(reps, func() {
				scalingCommit(t, s, nil, `{ctr: 1, tag: "t"}`, m)
			})
			t.Logf("  N=%4d  %v", n, d)
			s.Close()
		}

		t.Logf("%s SCOPED write, after N scope writes:", label)
		for _, n := range sizes {
			s, _ := setupStore(t, 1, n, &scope)
			var m *api.PathData
			if conditional {
				m = matchTag(t)
			}
			d := timeN(reps, func() {
				scalingCommit(t, s, &scope, `{ctr: 1, tag: "t"}`, m)
			})
			t.Logf("  N=%4d  %v", n, d)
			s.Close()
		}
	}
}

// TestScaling_WatchPerEvent measures what a watcher pays per delivered event: a
// baseline watcher steps its document by the committed patch (tony.Patch), while a
// scoped watcher recomputes the scoped view (ReadStateAt) at every event. This
// measures the two per-event costs directly, at the same accumulated write counts.
func TestScaling_WatchPerEvent(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	scope := "sandbox"
	sizes := []int{50, 100, 200, 400}
	const reps = 20

	patch, err := parse.Parse([]byte(`{ctr: 1, tag: "t"}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Log("baseline watcher per event (step the document by the committed patch):")
	for _, n := range sizes {
		s, commit := setupStore(t, n, 0, &scope)
		doc, err := s.ReadStateAt("", commit, nil)
		if err != nil {
			t.Fatalf("seed read: %v", err)
		}
		cur := doc
		d := timeN(reps, func() {
			stepped, err := tonyPatch(cur, patch)
			if err != nil {
				t.Fatalf("step: %v", err)
			}
			cur = stepped
		})
		t.Logf("  N=%4d  %v", n, d)
		s.Close()
	}

	t.Log("scoped watcher per event (recompute the scoped view):")
	for _, n := range sizes {
		s, commit := setupStore(t, 1, n, &scope)
		d := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, &scope); err != nil {
				t.Fatalf("recompute: %v", err)
			}
		})
		t.Logf("  N=%4d  %v", n, d)
		s.Close()
	}
}

// tonyPatch is the fold a baseline watcher uses, isolated here so the watch
// measurement does not depend on the server package.
func tonyPatch(base, patch *ir.Node) (*ir.Node, error) {
	if base == nil {
		base = ir.Null()
	}
	return tony.Patch(base, patch)
}
