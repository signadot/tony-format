package storage

import (
	"fmt"
	"testing"
	"time"
)

func TestDiagScopeCompaction(t *testing.T) {
	s := openTestStorage(t)
	scope := "s1"
	for i := 1; i <= 4; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	sc := mustCommit(t, s, &scope, `{demo: {x: {scoped: "keep me"}}}`)
	for i := 5; i <= 7; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{demo: {x: {hot: %d}}}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	t.Logf("scopeCommit=%d inactive=%s", sc, s.dLog.GetInactiveLog())
	dump := func(label string) {
		for _, seg := range s.index.LookupRangeAll("", nil, nil) {
			sid := "nil"
			if seg.ScopeID != nil {
				sid = *seg.ScopeID
			}
			t.Logf("  %s root: commit=%d tx=%d scope=%s %s@%d gen=%d", label, seg.StartCommit, seg.StartTx, sid, seg.LogFile, seg.LogPosition, seg.LogFileGeneration)
		}
	}
	dump("BEFORE")
	cfg := &CompactionConfig{Cutoff: 0, BaseInterval: time.Hour, SlotsPerTier: 8, Multiplier: 2, GracePeriod: 100 * time.Millisecond}
	if err := s.Compact(cfg); err != nil {
		t.Fatal(err)
	}
	dump("AFTER")
}
