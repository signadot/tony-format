package index

import (
	"sync"
	"testing"
)

// TestConcurrentAddAndLookupRange tests for race conditions when concurrently
// adding segments and calling LookupRange.
func TestConcurrentAddAndLookupRange(t *testing.T) {
	idx := NewIndex("")
	const numWriters = 2
	const numReaders = 10
	const segmentsPerWriter = 10

	var wg sync.WaitGroup
	writeResults := make([]int64, numWriters*segmentsPerWriter)

	// Concurrent writers: add segments
	for writerID := 0; writerID < numWriters; writerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < segmentsPerWriter; i++ {
				commitNum := int64(id*segmentsPerWriter + i + 1)
				txSeq := commitNum
				seg := PointLogSegment(commitNum, txSeq, "")
				idx.Add(seg)
				writeResults[commitNum-1] = commitNum
			}
		}(writerID)
	}

	// Concurrent readers: call LookupRange
	readResults := make([][]LogSegment, numReaders)
	for readerID := 0; readerID < numReaders; readerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Read multiple times as segments are being added
			for i := 0; i < 50; i++ {
				segments := idx.LookupRange("", nil, nil, nil)
				if len(segments) > 0 {
					// Store a copy of the segments
					segCopy := make([]LogSegment, len(segments))
					copy(segCopy, segments)
					readResults[id] = segCopy
				}
			}
		}(readerID)
	}

	wg.Wait()

	// Verify final state: all segments should be present
	finalSegments := idx.LookupRange("", nil, nil, nil)
	if len(finalSegments) != numWriters*segmentsPerWriter {
		t.Errorf("expected %d segments, got %d", numWriters*segmentsPerWriter, len(finalSegments))
	}

	// Verify segments are in order
	for i := 0; i < len(finalSegments)-1; i++ {
		if finalSegments[i].StartCommit > finalSegments[i+1].StartCommit {
			t.Errorf("segments out of order: commit %d before %d",
				finalSegments[i].StartCommit, finalSegments[i+1].StartCommit)
		}
	}
}

// TestConcurrentAddAndLookupRangeWithPath tests the same but with a virtual path
// to exercise the child index logic.
func TestConcurrentAddAndLookupRangeWithPath(t *testing.T) {
	idx := NewIndex("")
	const numWriters = 2
	const numReaders = 10
	const segmentsPerWriter = 10
	const testPath = "test/concurrent-path"

	var wg sync.WaitGroup
	writeResults := make(map[int64]bool)

	// Concurrent writers: add segments to the same path
	for writerID := 0; writerID < numWriters; writerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < segmentsPerWriter; i++ {
				commitNum := int64(id*segmentsPerWriter + i + 1)
				txSeq := commitNum
				seg := PointLogSegment(commitNum, txSeq, testPath)
				idx.Add(seg)
				idx.Lock()
				writeResults[commitNum] = true
				idx.Unlock()
			}
		}(writerID)
	}

	// Concurrent readers: call LookupRange on the path
	readErrors := make([]error, numReaders)
	for readerID := 0; readerID < numReaders; readerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Read multiple times as segments are being added
			for i := 0; i < 50; i++ {
				segments := idx.LookupRange(testPath, nil, nil, nil)
				// Verify segments are valid (no nil pointers, valid commit numbers)
				// With new semantics: StartCommit = LastCommit, EndCommit = Commit
				// StartCommit can be 0 for first commit, and StartCommit != EndCommit for patches
				for j := range segments {
					seg := &segments[j]
					if seg.StartCommit < 0 {
						readErrors[id] = &testError{msg: "invalid StartCommit"}
						return
					}
					if seg.EndCommit <= seg.StartCommit {
						readErrors[id] = &testError{msg: "EndCommit must be > StartCommit"}
						return
					}
				}
				// Verify segments are in order
				for j := 0; j < len(segments)-1; j++ {
					if segments[j].StartCommit > segments[j+1].StartCommit {
						readErrors[id] = &testError{msg: "segments out of order"}
						return
					}
				}
			}
		}(readerID)
	}

	wg.Wait()

	// Check for read errors
	for i, err := range readErrors {
		if err != nil {
			t.Errorf("reader %d encountered error: %v", i, err)
		}
	}

	// Verify final state: all segments should be present
	finalSegments := idx.LookupRange(testPath, nil, nil, nil)
	expectedCount := numWriters * segmentsPerWriter
	if len(finalSegments) != expectedCount {
		t.Errorf("expected %d segments, got %d", expectedCount, len(finalSegments))
	}

	// Verify all expected commits are present
	// With new semantics, check EndCommit (the commit where patch is applied)
	seenCommits := make(map[int64]bool)
	for _, seg := range finalSegments {
		if seenCommits[seg.EndCommit] {
			t.Errorf("duplicate commit %d in results", seg.EndCommit)
		}
		seenCommits[seg.EndCommit] = true
		if !writeResults[seg.EndCommit] {
			t.Errorf("commit %d in results but not in writeResults", seg.EndCommit)
		}
	}

	// Verify segments are in order
	for i := 0; i < len(finalSegments)-1; i++ {
		if finalSegments[i].StartCommit > finalSegments[i+1].StartCommit {
			t.Errorf("segments out of order: commit %d before %d",
				finalSegments[i].StartCommit, finalSegments[i+1].StartCommit)
		}
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
