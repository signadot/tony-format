package dlog

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// fencedLog is a DLog whose inactive log A holds n entries, commits 1..n, at positions.
func fencedLog(t *testing.T, n int) (*DLog, []int64) {
	t.Helper()
	dl, err := NewDLog(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewDLog: %v", err)
	}
	t.Cleanup(func() { dl.Close() })
	positions := make([]int64, 0, n)
	for i := 1; i <= n; i++ {
		pos, _, err := dl.AppendEntry(&Entry{
			Commit:    int64(i),
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"key": ir.FromInt(int64(i))}),
		})
		if err != nil {
			t.Fatalf("AppendEntry: %v", err)
		}
		positions = append(positions, pos)
	}
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive: %v", err)
	}
	return dl, positions
}

// A read indexed under the generation a compaction replaces answers the entry it was
// indexed for, while the compaction runs and after it, until the next. The generation was
// checked before the file's lock and bumped after the swap released it, so a read in
// between read the rewritten file at a position from the old one: in a probe, 385 of 800
// reads returned another record and 415 failed (d2mq819wh12ksynxmdn0).
func TestAReadAcrossACompactionAnswersTheEntryItWasIndexedFor(t *testing.T) {
	const n = 200
	dl, positions := fencedLog(t, n)
	gen := dl.GetGeneration(LogFileA)

	var wrong, failed, reads atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for k := r; ; k = (k + 7) % n {
				select {
				case <-stop:
					return
				default:
				}
				e, err := dl.ReadEntryAt(LogFileA, positions[k], gen)
				reads.Add(1)
				switch {
				case err != nil:
					failed.Add(1)
				case e.Commit != int64(k+1):
					wrong.Add(1)
				}
			}
		}(r)
	}

	// Keep every other entry: the survivors move, the rest go.
	var keep []int64
	for i := 1; i < n; i += 2 {
		keep = append(keep, positions[i])
	}
	if _, err := dl.CompactInactive(keep, &CompactConfig{GracePeriod: 10 * time.Millisecond}); err != nil {
		t.Fatalf("CompactInactive: %v", err)
	}
	time.Sleep(20 * time.Millisecond) // reads after the swap, at the old generation
	close(stop)
	wg.Wait()
	if wrong.Load() != 0 || failed.Load() != 0 {
		t.Errorf("%d of %d reads at the replaced generation answered another record, %d failed",
			wrong.Load(), reads.Load(), failed.Load())
	}

	// A second compaction retires the first one's file, and its generation with it.
	if _, err := dl.CompactInactive(nil, &CompactConfig{GracePeriod: 10 * time.Millisecond}); err != nil {
		t.Fatalf("second CompactInactive: %v", err)
	}
	if _, err := dl.ReadEntryAt(LogFileA, positions[0], gen); !errors.Is(err, ErrCompactionInterrupted) {
		t.Errorf("a read two compactions stale answered %v, want ErrCompactionInterrupted", err)
	}
}

// A section reader opened before a compaction reads on through it. The swap closed the
// handle it held as it began, so the reader's next read failed with "file already
// closed", whatever the grace period said (d2mq819wh12ksynxmdn0).
func TestASectionReaderReadsOnThroughACompaction(t *testing.T) {
	dl, positions := fencedLog(t, 20)
	gen := dl.GetGeneration(LogFileA)
	r, err := dl.OpenReaderAt(LogFileA, positions[0], gen)
	if err != nil {
		t.Fatalf("OpenReaderAt: %v", err)
	}
	first := make([]byte, 4)
	if _, err := io.ReadFull(r, first); err != nil {
		t.Fatalf("read before compaction: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := dl.CompactInactive(positions[10:], &CompactConfig{GracePeriod: 50 * time.Millisecond})
		done <- err
	}()
	if err := <-done; err != nil {
		t.Fatalf("CompactInactive: %v", err)
	}
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read after compaction: %v", err)
	}
	if len(rest) == 0 {
		t.Error("the reader read nothing after the compaction")
	}
	r.Close()
}
