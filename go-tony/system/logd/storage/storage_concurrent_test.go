package storage

import (
	"fmt"
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/compact"
)

// TestConcurrentCommitAndReadStateAt tests for race conditions when concurrently
// committing transactions and reading state at specific commits.
// This isolates the commit + ReadStateAt interaction without the full transaction machinery.
func TestConcurrentCommitAndReadStateAt(t *testing.T) {
	tmpDir := t.TempDir()
	compactorConfig := &compact.Config{
		Divisor: 1000,
		Remove:  compact.NeverRemove(),
	}
	storage, err := Open(tmpDir, 022, nil, compactorConfig)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	// Disable cache to rule out cache-related race conditions
	storage.stateCache = nil

	const numWriters = 10
	const segmentsPerWriter = 2
	const testPath = "/test/concurrent-path"

	var wg sync.WaitGroup
	writeResults := make(map[int64]int) // commit -> writerID

	// Concurrent writers: write diffs and commit them
	for writerID := 0; writerID < numWriters; writerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < segmentsPerWriter; i++ {
				// Create a diff with writer ID
				diff := ir.FromMap(map[string]*ir.Node{
					"writer": ir.FromInt(int64(id)),
					"value":  ir.FromString("value"),
				})

				// Write diff directly (bypassing transaction machinery)
				txSeq, err := storage.NextTxSeq()
				if err != nil {
					t.Errorf("writer %d: failed to get txSeq: %v", id, err)
					return
				}

				// Write pending file
				err = storage.WriteDiff(testPath, 0, txSeq, "", diff, true)
				if err != nil {
					t.Errorf("writer %d: failed to write diff: %v", id, err)
					return
				}

				// Commit
				commit, err := storage.NextCommit()
				if err != nil {
					t.Errorf("writer %d: failed to get commit: %v", id, err)
					return
				}

				err = storage.commit(testPath, txSeq, commit)
				if err != nil {
					t.Errorf("writer %d: failed to commit: %v", id, err)
					return
				}

				storage.Lock()
				writeResults[commit] = id
				storage.Unlock()
			}
		}(writerID)
	}

	// Concurrent readers: read state at various commits
	readErrors := make([]error, 2)
	for readerID := 0; readerID < 2; readerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Read a few times as commits are happening
			for i := 0; i < 3; i++ {
				// Get current commit to know what's available
				currentCommit, err := storage.GetCurrentCommit()
				if err != nil || currentCommit == 0 {
					continue
				}
				// Read a few random commits that exist, not all of them
				// Pick a commit that's likely to exist (somewhere in the middle)
				commitNum := currentCommit / 2
				if commitNum == 0 {
					commitNum = 1
				}
				
				// Check if commit exists in index before reading
				// GetCurrentCommit() reads from sequencer, but ReadStateAt reads from index.
				// There's a race where GetCurrentCommit() might return a commit that hasn't
				// been added to the index yet. Check index first to avoid this race.
				storage.indexMu.RLock()
				segments := storage.index.LookupRange(testPath, &commitNum, &commitNum)
				storage.indexMu.RUnlock()
				
				// Skip if commit isn't in index yet (race condition - commit() is still running)
				if len(segments) == 0 {
					continue
				}
				
				state, err := storage.ReadStateAt(testPath, commitNum)
				if err != nil {
					// Error reading - this is a problem
					readErrors[id] = &testError{
						msg:    "error reading state",
						commit: commitNum,
					}
					return
				}
				if state == nil {
					// Null state for a commit that should exist - this is a problem
					readErrors[id] = &testError{
						msg:    "nil state for existing commit",
						commit: commitNum,
					}
					return
				}
				// Verify state is valid - if commit exists, it should be ObjectType
				if state.Type != ir.ObjectType {
					readErrors[id] = &testError{
						msg:       "invalid state type",
						commit:    commitNum,
						stateType: state.Type,
					}
					return
				}
			}
		}(readerID)
	}

	wg.Wait()
	t.Logf("readers and writers complete")

	// Check for read errors
	for i, err := range readErrors {
		if err != nil {
			t.Errorf("reader %d encountered error: %v", i, err)
		}
	}

	// Verify final state: all commits should be readable
	finalCommit, err := storage.GetCurrentCommit()
	if err != nil {
		t.Fatalf("failed to get current commit: %v", err)
	}

	expectedCommits := int64(numWriters * segmentsPerWriter)
	if finalCommit != expectedCommits {
		t.Errorf("expected %d commits, got %d", expectedCommits, finalCommit)
	}

	// Verify each commit is readable and contains correct data
	for commitNum := int64(1); commitNum <= finalCommit; commitNum++ {
		state, err := storage.ReadStateAt(testPath, commitNum)
		if err != nil {
			t.Errorf("failed to read state at commit %d: %v", commitNum, err)
			continue
		}
		if state == nil {
			t.Errorf("state at commit %d is nil", commitNum)
			continue
		}
		if state.Type != ir.ObjectType {
			t.Errorf("expected object type at commit %d, got %v", commitNum, state.Type)
			continue
		}

		// Verify writer field exists
		storage.Lock()
		expectedWriterID := writeResults[commitNum]
		storage.Unlock()

		var actualWriterID *int64
		for j, fieldName := range state.Fields {
			if fieldName.String == "writer" && state.Values[j].Int64 != nil {
				actualWriterID = state.Values[j].Int64
				break
			}
		}

		if actualWriterID == nil {
			t.Errorf("commit %d: missing 'writer' field", commitNum)
			continue
		}

		if *actualWriterID != int64(expectedWriterID) {
			t.Errorf("commit %d: expected writer %d, got %d", commitNum, expectedWriterID, *actualWriterID)
		}
	}

	// Shutdown compaction goroutines before test teardown.
	// Shutdown() waits for acknowledgment that all goroutines have exited.
	storage.compactor.Shutdown()
}

type testError struct {
	msg       string
	commit    int64
	stateType ir.Type
}

func (e *testError) Error() string {
	if e.commit > 0 {
		msg := e.msg + " at commit " + fmt.Sprintf("%d", e.commit)
		if e.stateType != 0 {
			msg += " (type: " + e.stateType.String() + ")"
		}
		return msg
	}
	return e.msg
}
