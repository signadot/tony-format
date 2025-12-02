package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/compact"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

// Hypothesis 1: Filename mismatch between commit() and readDiffLocked()
// Test that the filename commit() creates matches what readDiffLocked() expects
func TestFilenameConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	storage.stateCache = nil

	testPath := "/test/path"
	// Use sequencer to get txSeq and commitCount
	txSeq, err := storage.NextTxSeq()
	if err != nil {
		t.Fatalf("NextTxSeq failed: %v", err)
	}
	commitCount, err := storage.NextCommit()
	if err != nil {
		t.Fatalf("NextCommit failed: %v", err)
	}

	// Write pending file
	diff := ir.FromMap(map[string]*ir.Node{"key": ir.FromString("value")})
	err = storage.WriteDiff(testPath, 0, txSeq, "", diff, true)
	if err != nil {
		t.Fatalf("failed to write pending diff: %v", err)
	}

	// Commit and capture the filename that was created
	fsPath := storage.FS.PathToFilesystem(testPath)
	pendingSeg := index.PointLogSegment(0, txSeq, "")
	oldFormatted := paths.FormatLogSegment(pendingSeg.AsPending(), 0, true)
	_, oldName := filepath.Split(oldFormatted)
	oldPath := filepath.Join(fsPath, oldName)

	// Verify pending file exists
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("pending file should exist: %v", err)
	}

	// Commit
	err = storage.commit(testPath, txSeq, commitCount)
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Now verify the committed filename matches what readDiffLocked would construct
	storage.indexMu.RLock()
	diffFile, err := storage.readDiffLocked(testPath, commitCount, txSeq, false)
	storage.indexMu.RUnlock()

	if err != nil {
		t.Fatalf("readDiffLocked failed: %v", err)
	}
	if diffFile == nil {
		t.Fatal("readDiffLocked returned nil")
	}

	// Verify the segment in index matches what we committed
	segments := storage.index.LookupRange(testPath, nil, nil)
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	seg := segments[0]
	if seg.StartCommit != commitCount {
		t.Errorf("segment StartCommit mismatch: expected %d, got %d", commitCount, seg.StartCommit)
	}
	if seg.StartTx != txSeq {
		t.Errorf("segment StartTx mismatch: expected %d, got %d", txSeq, seg.StartTx)
	}

	// Shutdown compaction goroutines before test teardown.
	// Shutdown() waits for acknowledgment that all goroutines have exited.
	storage.compactor.Shutdown()
}

// Hypothesis 2: File visibility race - file rename isn't immediately visible
// Test that after commit() returns, the file is immediately readable
func TestFileVisibilityAfterCommit(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	storage.stateCache = nil

	testPath := "/test/path"
	const numCommits = 10

	var wg sync.WaitGroup
	errors := make([]error, numCommits)

	// Concurrent commits
	for i := 0; i < numCommits; i++ {
		wg.Add(1)
		go func(commitNum int) {
			defer wg.Done()
			// Use sequencer to get txSeq and commitCount
			txSeq, err := storage.NextTxSeq()
			if err != nil {
				errors[commitNum] = fmt.Errorf("NextTxSeq failed: %w", err)
				return
			}
			commitCount, err := storage.NextCommit()
			if err != nil {
				errors[commitNum] = fmt.Errorf("NextCommit failed: %w", err)
				return
			}

			diff := ir.FromMap(map[string]*ir.Node{
				"commit": ir.FromInt(commitCount),
			})

			// Write pending
			err = storage.WriteDiff(testPath, 0, txSeq, "", diff, true)
			if err != nil {
				errors[commitNum] = fmt.Errorf("write failed: %w", err)
				return
			}

			// Commit
			err = storage.commit(testPath, txSeq, commitCount)
			if err != nil {
				errors[commitNum] = fmt.Errorf("commit failed: %w", err)
				return
			}

			// Immediately try to read the file (without holding any locks)
			// This tests if the file is visible immediately after commit returns
			storage.indexMu.RLock()
			_, readErr := storage.readDiffLocked(testPath, commitCount, txSeq, false)
			storage.indexMu.RUnlock()

			if readErr != nil {
				errors[commitNum] = fmt.Errorf("immediate read failed: %w", readErr)
			}
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("commit %d: %v", i+1, err)
		}
	}

	// Shutdown compaction goroutines before test teardown.
	// Shutdown() waits for acknowledgment that all goroutines have exited.
	// Give compaction a moment to start processing before shutdown.
	time.Sleep(100 * time.Millisecond)
	storage.compactor.Shutdown()
	// Shutdown() now waits for doneAck, so no need to sleep after
}

// Hypothesis 3: Index segment values don't match what was committed
// Test that segments added to index have correct commit/txSeq values
func TestIndexSegmentValues(t *testing.T) {
	tmpDir := t.TempDir()
	compactorConfig := &compact.Config{
		Divisor: 1000,
		Remove:  compact.NeverRemove(),
	}
	storage, err := Open(tmpDir, 022, nil, compactorConfig)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	storage.stateCache = nil

	testPath := "/test/path"
	const numCommits = 20

	commits := make(map[int64]int64) // commit -> txSeq
	var commitsMu sync.Mutex

	var wg sync.WaitGroup
	errors := make([]error, numCommits)

	// Concurrent commits
	for i := 0; i < numCommits; i++ {
		wg.Add(1)
		go func(commitNum int) {
			defer wg.Done()
			txSeq, err := storage.NextTxSeq()
			if err != nil {
				errors[commitNum] = fmt.Errorf("NextTxSeq failed: %w", err)
				return
			}

			commitCount, err := storage.NextCommit()
			if err != nil {
				errors[commitNum] = fmt.Errorf("NextCommit failed: %w", err)
				return
			}

			commitsMu.Lock()
			commits[commitCount] = txSeq
			commitsMu.Unlock()

			diff := ir.FromMap(map[string]*ir.Node{
				"commit": ir.FromInt(commitCount),
			})

			err = storage.WriteDiff(testPath, 0, txSeq, "", diff, true)
			if err != nil {
				errors[commitNum] = fmt.Errorf("write failed: %w", err)
				return
			}

			err = storage.commit(testPath, txSeq, commitCount)
			if err != nil {
				errors[commitNum] = fmt.Errorf("commit failed: %w", err)
				return
			}
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("commit %d: %v", i+1, err)
		}
	}

	// Verify all segments in index match what was committed
	storage.indexMu.RLock()
	segments := storage.index.LookupRange(testPath, nil, nil)
	storage.indexMu.RUnlock()

	if len(segments) != numCommits {
		t.Errorf("expected %d segments, got %d", numCommits, len(segments))
	}

	commitsMu.Lock()
	for _, seg := range segments {
		expectedTxSeq, ok := commits[seg.StartCommit]
		if !ok {
			t.Errorf("segment with commit %d not found in commits map", seg.StartCommit)
			continue
		}
		if seg.StartTx != expectedTxSeq {
			t.Errorf("segment commit %d: expected txSeq %d, got %d", seg.StartCommit, expectedTxSeq, seg.StartTx)
		}
		// CRITICAL: Verify no segments have StartCommit=0
		if seg.StartCommit == 0 {
			t.Errorf("segment has StartCommit=0! txSeq=%d, RelPath=%q", seg.StartTx, seg.RelPath)
		}
	}
	commitsMu.Unlock()

	// Shutdown compaction goroutines before test teardown.
	// Shutdown() waits for acknowledgment that all goroutines have exited.
	storage.compactor.Shutdown()
}

// Hypothesis 4: ReadStateAt sees segments with StartCommit=0
// Test that ReadStateAt doesn't get segments with StartCommit=0 when reading at commit > 0
func TestReadStateAtNoZeroCommits(t *testing.T) {
	tmpDir := t.TempDir()
	compactorConfig := &compact.Config{
		Divisor: 1000,
		Remove:  compact.NeverRemove(),
	}
	storage, err := Open(tmpDir, 022, nil, compactorConfig)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	storage.stateCache = nil

	testPath := "/test/path"
	const numCommits = 5

	// Sequentially commit (no concurrency) to establish baseline
	for i := 0; i < numCommits; i++ {
		txSeq, err := storage.NextTxSeq()
		if err != nil {
			t.Fatalf("NextTxSeq failed: %v", err)
		}
		commitCount, err := storage.NextCommit()
		if err != nil {
			t.Fatalf("NextCommit failed: %v", err)
		}

		diff := ir.FromMap(map[string]*ir.Node{
			"value": ir.FromInt(commitCount),
		})

		err = storage.WriteDiff(testPath, 0, txSeq, "", diff, true)
		if err != nil {
			t.Fatalf("WriteDiff failed: %v", err)
		}

		err = storage.commit(testPath, txSeq, commitCount)
		if err != nil {
			t.Fatalf("commit failed: %v", err)
		}
	}

	// Verify segments in index don't have StartCommit=0
	storage.indexMu.RLock()
	allSegments := storage.index.LookupRange(testPath, nil, nil)
	storage.indexMu.RUnlock()

	for _, seg := range allSegments {
		if seg.StartCommit == 0 {
			t.Errorf("found segment with StartCommit=0: txSeq=%d, RelPath=%q", seg.StartTx, seg.RelPath)
		}
	}

	// Now try to read at commit 1 - should not see any segments with StartCommit=0
	state, err := storage.ReadStateAt(testPath, 1)
	if err != nil {
		t.Fatalf("ReadStateAt failed: %v", err)
	}
	if state == nil {
		t.Fatal("ReadStateAt returned nil")
	}

	// Verify we got the correct state
	if state.Type != ir.ObjectType {
		t.Errorf("expected ObjectType, got %v", state.Type)
	}

	// Shutdown compaction goroutines before test teardown.
	// Shutdown() waits for acknowledgment that all goroutines have exited.
	storage.compactor.Shutdown()
}

// Hypothesis 5: Concurrent commits interfere with each other
// Test that multiple concurrent commits don't corrupt each other's files
func TestConcurrentCommitsNoInterference(t *testing.T) {
	tmpDir := t.TempDir()
	compactorConfig := &compact.Config{
		Divisor: 1000,
		Remove:  compact.NeverRemove(),
	}
	storage, err := Open(tmpDir, 022, nil, compactorConfig)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	storage.stateCache = nil

	testPath := "/test/path"
	const numWriters = 5
	const commitsPerWriter = 10

	var wg sync.WaitGroup
	writerCommits := make(map[int]map[int64]int64) // writerID -> commit -> txSeq
	var writerCommitsMu sync.Mutex

	// Concurrent writers
	for writerID := 0; writerID < numWriters; writerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			commits := make(map[int64]int64)

			for i := 0; i < commitsPerWriter; i++ {
				txSeq, err := storage.NextTxSeq()
				if err != nil {
					t.Errorf("writer %d: NextTxSeq failed: %v", id, err)
					return
				}

				commitCount, err := storage.NextCommit()
				if err != nil {
					t.Errorf("writer %d: NextCommit failed: %v", id, err)
					return
				}

				commits[commitCount] = txSeq

				diff := ir.FromMap(map[string]*ir.Node{
					"writer": ir.FromInt(int64(id)),
					"seq":    ir.FromInt(int64(i)),
				})

				err = storage.WriteDiff(testPath, 0, txSeq, "", diff, true)
				if err != nil {
					t.Errorf("writer %d: WriteDiff failed: %v", id, err)
					return
				}

				err = storage.commit(testPath, txSeq, commitCount)
				if err != nil {
					t.Errorf("writer %d: commit failed: %v", id, err)
					return
				}
			}

			writerCommitsMu.Lock()
			writerCommits[id] = commits
			writerCommitsMu.Unlock()
		}(writerID)
	}

	wg.Wait()

	// Verify all commits are readable and have correct values
	storage.indexMu.RLock()
	segments := storage.index.LookupRange(testPath, nil, nil)
	storage.indexMu.RUnlock()

	expectedTotalCommits := numWriters * commitsPerWriter
	if len(segments) != expectedTotalCommits {
		t.Errorf("expected %d segments, got %d", expectedTotalCommits, len(segments))
	}

	// Verify each segment can be read and has correct values
	for _, seg := range segments {
		// CRITICAL: Verify no segments have StartCommit=0
		if seg.StartCommit == 0 {
			t.Errorf("segment has StartCommit=0! txSeq=%d, RelPath=%q", seg.StartTx, seg.RelPath)
			continue
		}

		storage.indexMu.RLock()
		diffFile, err := storage.readDiffLocked(testPath, seg.StartCommit, seg.StartTx, false)
		storage.indexMu.RUnlock()

		if err != nil {
			t.Errorf("failed to read segment commit %d txSeq %d: %v", seg.StartCommit, seg.StartTx, err)
			continue
		}

		// Find which writer this commit belongs to
		foundWriter := -1
		writerCommitsMu.Lock()
		for writerID, commits := range writerCommits {
			if txSeq, ok := commits[seg.StartCommit]; ok && txSeq == seg.StartTx {
				foundWriter = writerID
				break
			}
		}
		writerCommitsMu.Unlock()

		if foundWriter == -1 {
			t.Errorf("commit %d txSeq %d: could not find writer", seg.StartCommit, seg.StartTx)
			continue
		}

		// Verify diff file contains correct writer ID
		if diffFile.Diff == nil || diffFile.Diff.Type != ir.ObjectType {
			t.Errorf("commit %d: invalid diff", seg.StartCommit)
			continue
		}

		var foundWriterID *int64
		for j, fieldName := range diffFile.Diff.Fields {
			if fieldName.String == "writer" && diffFile.Diff.Values[j].Int64 != nil {
				foundWriterID = diffFile.Diff.Values[j].Int64
				break
			}
		}

		if foundWriterID == nil {
			t.Errorf("commit %d: missing writer field", seg.StartCommit)
			continue
		}

		if *foundWriterID != int64(foundWriter) {
			t.Errorf("commit %d: expected writer %d, got %d", seg.StartCommit, foundWriter, *foundWriterID)
		}
	}

	// Shutdown compaction goroutines before test teardown.
	// Shutdown() waits for acknowledgment that all goroutines have exited.
	storage.compactor.Shutdown()
}
