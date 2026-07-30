package storage

import (
	"fmt"
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/seq"
)

// commitValue writes src at the root and returns the commit it landed at.
func commitValue(t *testing.T, s *Storage, src string) int64 {
	t.Helper()
	tx, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	data, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	r := p.Commit()
	if !r.Committed {
		t.Fatalf("commit %q failed: %v", src, r.Error)
	}
	return r.Commit
}

// seed opens a store, writes n commits, and closes it, returning the last commit.
func seedCommits(t *testing.T, dir string, n int) int64 {
	t.Helper()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	var last int64
	for i := range n {
		last = commitValue(t, s, fmt.Sprintf("{k%d: %d}", i, i))
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return last
}

// A commit number must name one state forever — every watch cursor rests on it, since
// a client resumes by saying "I have through commit N". The counter and the log are
// separate files and neither is fsynced on the commit path, so a crash can leave the
// counter BEHIND the log; the next commit then reuses a number the log already holds.
// Losing meta/seq outright is the extreme case: the whole sequence restarted from 1.
func TestStorage_ReopenDoesNotReissueCommits_SeqLost(t *testing.T) {
	dir := t.TempDir()
	last := seedCommits(t, dir, 3)

	if err := os.Remove(seq.NewSeq(dir).StateFilePath()); err != nil {
		t.Fatalf("remove seq: %v", err)
	}

	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	got, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if got != last {
		t.Errorf("watermark after reopen = %d, want %d (the log's last commit)", got, last)
	}
	if next := commitValue(t, s, `{after: 1}`); next <= last {
		t.Errorf("next commit = %d, reissues a number the log already holds (last = %d)", next, last)
	}
}

// The same hole with the counter merely rewound rather than lost, which is what a
// partially-flushed rename leaves behind.
func TestStorage_ReopenDoesNotReissueCommits_SeqRewound(t *testing.T) {
	dir := t.TempDir()
	last := seedCommits(t, dir, 4)

	rewind := &seq.State{Commit: 1, TxSeq: 1}
	sq := seq.NewSeq(dir)
	sq.Lock()
	err := sq.WriteStateLocked(rewind)
	sq.Unlock()
	if err != nil {
		t.Fatalf("rewind seq: %v", err)
	}

	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	if next := commitValue(t, s, `{after: 1}`); next <= last {
		t.Errorf("next commit = %d, reissues a number the log already holds (last = %d)", next, last)
	}
}

// Transaction ids are reconciled on the same grounds: they identify the writer of an
// entry in the index, so reusing one makes two entries indistinguishable.
func TestStorage_ReopenDoesNotReissueTxSeq(t *testing.T) {
	dir := t.TempDir()

	s1, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := range 3 {
		commitValue(t, s1, fmt.Sprintf("{k%d: %d}", i, i))
	}
	_, maxTx := s1.indexWatermarks()
	if maxTx == 0 {
		t.Fatal("expected a non-zero tx watermark after three commits")
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := os.Remove(seq.NewSeq(dir).StateFilePath()); err != nil {
		t.Fatalf("remove seq: %v", err)
	}

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	tx, err := s2.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	if tx.ID() <= maxTx {
		t.Errorf("new tx id = %d, reissues an id the log already holds (max = %d)", tx.ID(), maxTx)
	}
}

// Reconciliation must be inert on a healthy store: the counter is bumped before the
// append, so it is normally at or ahead of the log, and reopening must not invent a
// hole by moving it.
func TestStorage_ReopenLeavesHealthyWatermarkAlone(t *testing.T) {
	dir := t.TempDir()
	last := seedCommits(t, dir, 3)

	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	got, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if got != last {
		t.Errorf("watermark after clean reopen = %d, want %d unchanged", got, last)
	}
	if next := commitValue(t, s, `{after: 1}`); next != last+1 {
		t.Errorf("next commit = %d, want %d (no hole)", next, last+1)
	}
}

// A counter left AHEAD of the log is the benign direction: the unused numbers are a
// hole, and reconciliation must not pull the counter back down into the log.
func TestStorage_ReopenDoesNotLowerWatermark(t *testing.T) {
	dir := t.TempDir()
	last := seedCommits(t, dir, 2)

	ahead := &seq.State{Commit: last + 100, TxSeq: 500}
	sq := seq.NewSeq(dir)
	sq.Lock()
	err := sq.WriteStateLocked(ahead)
	sq.Unlock()
	if err != nil {
		t.Fatalf("advance seq: %v", err)
	}

	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	got, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if got != last+100 {
		t.Errorf("watermark after reopen = %d, want %d (unchanged, holes are benign)", got, last+100)
	}
}

func TestStorage_FreshStoreStartsAtOne(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(seq.NewSeq(dir).StateFilePath()); err != nil {
		t.Errorf("fresh store did not create the sequence file: %v", err)
	}
	if got, err := s.GetCurrentCommit(); err != nil || got != 0 {
		t.Errorf("fresh watermark = %d (err %v), want 0", got, err)
	}
	if first := commitValue(t, s, `{a: 1}`); first != 1 {
		t.Errorf("first commit = %d, want 1", first)
	}
}

func TestStorage_DurabilityDefaultsToOS(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	if got := s.GetDurability(); got != DurabilityOS {
		t.Errorf("default durability = %v, want %v", got, DurabilityOS)
	}
}

// DurabilitySync adds an fsync to the commit path; the commits themselves must still
// land, index, and read back identically.
func TestStorage_DurabilitySync(t *testing.T) {
	dir := t.TempDir()

	s1, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s1.SetDurability(DurabilitySync)
	if got := s1.GetDurability(); got != DurabilitySync {
		t.Fatalf("durability = %v, want %v", got, DurabilitySync)
	}

	var last int64
	for i := range 3 {
		last = commitValue(t, s1, fmt.Sprintf("{k%d: %d}", i, i))
	}
	before, err := s1.ReadStateAt("", last, nil)
	if err != nil {
		t.Fatalf("ReadStateAt: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	after, err := s2.ReadStateAt("", last, nil)
	if err != nil {
		t.Fatalf("ReadStateAt after reopen: %v", err)
	}
	if !after.DeepEqual(before) {
		t.Errorf("state at commit %d changed across reopen:\nbefore: %v\nafter:  %v", last, before, after)
	}
}

// Sync is available as an explicit flush point under the default mode.
func TestStorage_ExplicitSync(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	commitValue(t, s, `{a: 1}`)
	if err := s.Sync(); err != nil {
		t.Errorf("Sync: %v", err)
	}
}
