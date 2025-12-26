package server

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// mustParseStress parses a Tony document or panics.
func mustParseStress(s string) *ir.Node {
	node, err := parse.Parse([]byte(s))
	if err != nil {
		panic(err)
	}
	return node
}

// doPatchErr applies a patch via storage transaction and triggers onCommit.
// Returns an error instead of failing the test.
func doPatchErr(store *storage.Storage, srv *Server, path string, data *ir.Node) error {
	tx, err := store.NewTx(1, nil)
	if err != nil {
		return fmt.Errorf("NewTx failed: %w", err)
	}

	patcher, err := tx.NewPatcher(&api.Patch{
		Patch: api.PathData{
			Path: path,
			Data: data,
		},
	})
	if err != nil {
		return fmt.Errorf("NewPatcher failed: %w", err)
	}

	result := patcher.Commit()
	if result.Error != nil {
		return fmt.Errorf("Commit failed: %w", result.Error)
	}

	srv.onCommit()
	return nil
}

func TestReadsDuringSnapshot(t *testing.T) {
	dir, err := os.MkdirTemp("", "logd-stress-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := storage.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// No auto-snapshot - we'll trigger manually
	srv := New(&Spec{Storage: store})

	// Write initial data
	for i := 0; i < 10; i++ {
		data := mustParseStress(fmt.Sprintf(`{id: "%d", name: "User %d"}`, i, i))
		if err := doPatchErr(store, srv, fmt.Sprintf("users.user%d", i), data); err != nil {
			t.Fatal(err)
		}
	}

	// Start snapshot in background
	var snapshotErr error
	var snapshotDone sync.WaitGroup
	snapshotDone.Add(1)
	go func() {
		defer snapshotDone.Done()
		snapshotErr = store.SwitchAndSnapshot()
	}()

	// While snapshot runs, do reads
	commit, _ := store.GetCurrentCommit()
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("users.user%d", i%10)
		state, err := store.ReadStateAt(path, commit, nil)
		if err != nil {
			t.Errorf("Read during snapshot failed: %v", err)
		}
		if state == nil || state.Type == ir.NullType {
			t.Errorf("Empty response for %s", path)
		}
	}

	snapshotDone.Wait()
	if snapshotErr != nil {
		t.Errorf("Snapshot failed: %v", snapshotErr)
	}

	t.Log("Reads during snapshot succeeded")
}

func TestSnapshotStress(t *testing.T) {
	dir, err := os.MkdirTemp("", "logd-stress-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := storage.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// No auto-snapshot - we control it
	srv := New(&Spec{Storage: store})

	// Pre-populate data so reads don't fail on missing items
	for i := 0; i < 100; i++ {
		data := mustParseStress(fmt.Sprintf(`{id: "%d", value: "initial"}`, i))
		if err := doPatchErr(store, srv, fmt.Sprintf("data.item%d", i), data); err != nil {
			t.Fatalf("Failed to pre-populate item%d: %v", i, err)
		}
	}

	var (
		writeCount    atomic.Int64
		readCount     atomic.Int64
		snapshotCount atomic.Int64
		errors        atomic.Int64
		stop          atomic.Bool
	)

	var errorMsgs sync.Map

	// Writer goroutine
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for !stop.Load() {
			i := writeCount.Add(1)
			data := mustParseStress(fmt.Sprintf(`{id: "%d", value: "test data %d"}`, i, i))
			err := doPatchErr(store, srv, fmt.Sprintf("data.item%d", i%100), data)
			if err != nil {
				errors.Add(1)
				errorMsgs.Store(fmt.Sprintf("write-%d", i), fmt.Sprintf("PATCH %d: %v", i, err))
			}
		}
	}()

	// Reader goroutine
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for !stop.Load() {
			i := readCount.Add(1)
			commit, _ := store.GetCurrentCommit()
			_, err := store.ReadStateAt(fmt.Sprintf("data.item%d", i%100), commit, nil)
			if err != nil {
				errors.Add(1)
				errorMsgs.Store(fmt.Sprintf("read-%d", i), fmt.Sprintf("MATCH %d: %v", i, err))
			}
			time.Sleep(time.Microsecond * 100) // Slight delay to not overwhelm
		}
	}()

	// Snapshot triggerer - rapid fire
	snapshotterDone := make(chan struct{})
	go func() {
		defer close(snapshotterDone)
		for !stop.Load() {
			err := store.SwitchAndSnapshot()
			if err != nil {
				// ErrSnapshotInProgress is expected under stress
				t.Logf("Snapshot: %v", err)
			} else {
				snapshotCount.Add(1)
			}
			time.Sleep(time.Millisecond * 10) // Try to snapshot frequently
		}
	}()

	// Let it run for a bit
	time.Sleep(time.Second * 2)
	stop.Store(true)

	// Wait for all goroutines
	<-writerDone
	<-readerDone
	<-snapshotterDone

	t.Logf("Writes: %d, Reads: %d, Snapshots: %d, Errors: %d",
		writeCount.Load(), readCount.Load(), snapshotCount.Load(), errors.Load())

	if errors.Load() > 0 {
		t.Errorf("Had %d errors during stress test:", errors.Load())
		errorMsgs.Range(func(key, value interface{}) bool {
			t.Logf("  %s: %s", key, value)
			return true
		})
	}
	if snapshotCount.Load() < 2 {
		t.Errorf("Expected at least 2 snapshots, got %d", snapshotCount.Load())
	}
}

func TestConcurrentSnapshots(t *testing.T) {
	// This test verifies that we can have both log files being snapshotted
	// by using goroutines that race to trigger snapshots after switches.
	dir, err := os.MkdirTemp("", "logd-concurrent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := storage.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	srv := New(&Spec{Storage: store})

	// Write some data first
	for i := 0; i < 5; i++ {
		data := mustParseStress(fmt.Sprintf(`{id: "%d"}`, i))
		if err := doPatchErr(store, srv, fmt.Sprintf("init.item%d", i), data); err != nil {
			t.Fatalf("Initial write failed: %v", err)
		}
	}

	// Launch multiple goroutines trying to snapshot concurrently
	var wg sync.WaitGroup
	results := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Each goroutine tries to switch and snapshot
			err := store.SwitchAndSnapshot()
			results <- err
		}(i)
	}

	wg.Wait()
	close(results)

	successCount := 0
	inProgressCount := 0
	for err := range results {
		if err == nil {
			successCount++
		} else if strings.Contains(err.Error(), "snapshot already in progress") {
			inProgressCount++
		} else {
			t.Logf("Unexpected snapshot error: %v", err)
		}
	}

	t.Logf("Concurrent snapshot attempts: %d succeeded, %d got ErrSnapshotInProgress", successCount, inProgressCount)

	// We should have some successes and some "in progress" errors
	if successCount == 0 {
		t.Error("Expected at least one successful snapshot")
	}
}
