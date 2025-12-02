package storage

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/compact"
)

func TestWriteThenRead(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Write: Create transaction and commit a patch
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	patcher := tx.NewPatcher()

	// Create a simple diff: set /test/path to {"key": "value"}
	diff := ir.FromMap(map[string]*ir.Node{
		"key": ir.FromString("value"),
	})
	patch := createTestPatch("/test/path", diff, nil)

	isLast, err := patcher.AddPatch(patch)
	if err != nil {
		t.Fatalf("failed to add patch: %v", err)
	}
	if !isLast {
		t.Fatal("single participant should be last")
	}

	result := patcher.Commit()
	if !result.Committed {
		t.Fatal("transaction should be committed")
	}
	if result.Commit == 0 {
		t.Fatal("commit number should be non-zero")
	}
	commitNum := result.Commit

	// Read: Read back the state at the commit
	readState, err := storage.ReadStateAt("/test/path", commitNum)
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}

	// Verify: Check that the read state matches what we wrote
	if readState == nil {
		t.Fatal("read state should not be nil")
	}

	// Verify the map structure
	if readState.Type != ir.ObjectType {
		t.Errorf("expected object/map type, got %v", readState.Type)
	}

	// Verify the "key" field exists and has the correct value
	// For maps, Fields contains field names and Values contains corresponding values
	var keyNode *ir.Node
	for i, fieldName := range readState.Fields {
		if fieldName.String == "key" {
			keyNode = readState.Values[i]
			break
		}
	}
	if keyNode == nil {
		t.Fatal("expected 'key' field in read state")
	}
	if keyNode.Type != ir.StringType {
		t.Errorf("expected string type for 'key', got %v", keyNode.Type)
	}
	if keyNode.String != "value" {
		t.Errorf("expected 'key' value 'value', got %q", keyNode.String)
	}
}

func TestWriteManyPathsThenReadParallel(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Write: Create transactions and commit patches to many different paths
	const numPaths = 20
	paths := make([]string, numPaths)
	expectedValues := make(map[string]string)
	commits := make(map[string]int64)

	for i := 0; i < numPaths; i++ {
		path := fmt.Sprintf("/test/path%d", i)
		paths[i] = path
		value := fmt.Sprintf("value%d", i)
		expectedValues[path] = value

		// Create transaction
		tx, err := storage.NewTx(1)
		if err != nil {
			t.Fatalf("failed to create transaction for path %s: %v", path, err)
		}
		patcher := tx.NewPatcher()

		// Create diff: set path to {"key": value}
		diff := ir.FromMap(map[string]*ir.Node{
			"key": ir.FromString(value),
		})
		patch := createTestPatch(path, diff, nil)

		isLast, err := patcher.AddPatch(patch)
		if err != nil {
			t.Fatalf("failed to add patch for path %s: %v", path, err)
		}
		if !isLast {
			t.Fatalf("single participant should be last for path %s", path)
		}

		result := patcher.Commit()
		if !result.Committed {
			t.Fatalf("transaction should be committed for path %s", path)
		}
		if result.Commit == 0 {
			t.Fatalf("commit number should be non-zero for path %s", path)
		}
		commits[path] = result.Commit
	}

	// Read: Read all paths in parallel
	var wg sync.WaitGroup
	readErrors := make(map[string]error)
	readStates := make(map[string]*ir.Node)
	readMu := sync.Mutex{}

	for _, path := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()

			// Read using ReadStateAt with the specific commit
			state, err := storage.ReadStateAt(p, commits[p])
			readMu.Lock()
			defer readMu.Unlock()
			if err != nil {
				readErrors[p] = err
				return
			}
			readStates[p] = state
		}(path)
	}

	wg.Wait()

	// Verify: Check all reads succeeded and values match
	if len(readErrors) > 0 {
		for path, err := range readErrors {
			t.Errorf("failed to read path %s: %v", path, err)
		}
	}

	if len(readStates) != numPaths {
		t.Errorf("expected %d read states, got %d", numPaths, len(readStates))
	}

	for path, expectedValue := range expectedValues {
		state, ok := readStates[path]
		if !ok {
			t.Errorf("missing read state for path %s", path)
			continue
		}

		if state == nil {
			t.Errorf("read state is nil for path %s", path)
			continue
		}

		if state.Type != ir.ObjectType {
			t.Errorf("expected object/map type for path %s, got %v", path, state.Type)
			continue
		}

		// Verify the "key" field exists and has the correct value
		var keyNode *ir.Node
		for i, fieldName := range state.Fields {
			if fieldName.String == "key" {
				keyNode = state.Values[i]
				break
			}
		}
		if keyNode == nil {
			t.Errorf("expected 'key' field in read state for path %s", path)
			continue
		}
		if keyNode.Type != ir.StringType {
			t.Errorf("expected string type for 'key' in path %s, got %v", path, keyNode.Type)
			continue
		}
		if keyNode.String != expectedValue {
			t.Errorf("expected 'key' value %q for path %s, got %q", expectedValue, path, keyNode.String)
		}
	}
}

type writeResult struct {
	writerID int
	commit   int64
	value    string
	success  bool
}

func runConcurrentWriters(t *testing.T, storage *Storage, testPath string, numWriters int) []writeResult {
	var wg sync.WaitGroup
	var writeMu sync.Mutex
	writeResults := make([]writeResult, numWriters)

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		writerID := i
		value := fmt.Sprintf("value-%d", writerID)
		go func(id int, val string) {
			defer wg.Done()

			tx, err := storage.NewTx(1)
			if err != nil {
				writeMu.Lock()
				writeResults[id].writerID = id
				writeMu.Unlock()
				t.Errorf("writer %d: failed to create transaction: %v", id, err)
				return
			}
			patcher := tx.NewPatcher()

			writerIDInt64 := int64(id)
			diff := ir.FromMap(map[string]*ir.Node{
				"writer": ir.FromInt(writerIDInt64),
				"value":  ir.FromString(val),
			})
			patch := createTestPatch(testPath, diff, nil)

			isLast, err := patcher.AddPatch(patch)
			if err != nil {
				writeMu.Lock()
				writeResults[id].writerID = id
				writeMu.Unlock()
				t.Errorf("writer %d: failed to add patch: %v", id, err)
				return
			}
			if !isLast {
				t.Errorf("writer %d: single participant should be last", id)
				return
			}

			result := patcher.Commit()
			writeMu.Lock()
			writeResults[id].writerID = id
			writeResults[id].commit = result.Commit
			writeResults[id].value = val
			writeResults[id].success = result.Committed
			writeMu.Unlock()

			if !result.Committed {
				t.Errorf("writer %d: transaction should be committed", id)
			}
		}(writerID, value)
	}

	wg.Wait()
	return writeResults
}

func runConcurrentReaders(t *testing.T, storage *Storage, testPath string, numReaders int) []*ir.Node {
	var wg sync.WaitGroup
	var readMu sync.Mutex
	readStates := make([]*ir.Node, 0)

	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			maxReads := 50
			for j := 0; j < maxReads; j++ {
				// ReadCurrentState is atomic - it gets commit then reads at that commit
				state, err := storage.ReadCurrentState(testPath)
				if err == nil && state != nil {
					readMu.Lock()
					readStates = append(readStates, state)
					readMu.Unlock()
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	return readStates
}

func verifyAllWritesSucceeded(t *testing.T, writeResults []writeResult) {
	successCount := 0
	for i, result := range writeResults {
		if result.success {
			successCount++
		} else {
			t.Errorf("writer %d failed to commit", i)
		}
	}
	if successCount != len(writeResults) {
		t.Errorf("expected %d successful writes, got %d", len(writeResults), successCount)
	}
}

func verifyCommitSequence(t *testing.T, writeResults []writeResult) {
	commits := make([]int64, 0, len(writeResults))
	for _, result := range writeResults {
		if result.success && result.commit > 0 {
			commits = append(commits, result.commit)
		}
	}

	if len(commits) != len(writeResults) {
		t.Errorf("expected %d commits, got %d", len(writeResults), len(commits))
		return
	}

	// Sort commits to check sequence
	sort.Slice(commits, func(i, j int) bool {
		return commits[i] < commits[j]
	})

	// Check that commits are sequential (1, 2, 3, ...)
	for i := 0; i < len(commits); i++ {
		expectedCommit := int64(i + 1)
		if commits[i] != expectedCommit {
			t.Errorf("expected commit %d at position %d, got %d", expectedCommit, i, commits[i])
		}
	}
}

func verifyFinalState(t *testing.T, storage *Storage, testPath string, writeResults []writeResult) {
	finalState, err := storage.ReadCurrentState(testPath)
	if err != nil {
		t.Fatalf("failed to read final state: %v", err)
	}

	if finalState == nil {
		t.Fatal("final state should not be nil")
	}

	if finalState.Type != ir.ObjectType {
		t.Errorf("expected object/map type, got %v", finalState.Type)
		return
	}

	// Find the last writer's value
	lastWriterID := -1
	lastCommit := int64(0)
	for _, result := range writeResults {
		if result.success && result.commit > lastCommit {
			lastCommit = result.commit
			lastWriterID = result.writerID
		}
	}

	if lastWriterID == -1 {
		t.Fatal("could not determine last writer")
		return
	}

	// Verify final state contains the last writer's data
	var writerNode, valueNode *ir.Node
	for i, fieldName := range finalState.Fields {
		if fieldName.String == "writer" {
			writerNode = finalState.Values[i]
		}
		if fieldName.String == "value" {
			valueNode = finalState.Values[i]
		}
	}

	if writerNode == nil {
		t.Error("expected 'writer' field in final state")
	} else if writerNode.Int64 == nil {
		t.Errorf("expected 'writer' to be a number, got type=%v, String=%q, Fields=%v, Values=%v",
			writerNode.Type, writerNode.String, writerNode.Fields, writerNode.Values)
	} else if *writerNode.Int64 != int64(lastWriterID) {
		t.Errorf("expected final state writer %d, got %d", lastWriterID, *writerNode.Int64)
	}

	expectedFinalValue := fmt.Sprintf("value-%d", lastWriterID)
	if valueNode == nil {
		t.Error("expected 'value' field in final state")
	} else if valueNode.String != expectedFinalValue {
		t.Errorf("expected final state value %q, got %q", expectedFinalValue, valueNode.String)
	}
}

func verifyCommitStates(t *testing.T, storage *Storage, testPath string, writeResults []writeResult) {
	// Build a map of commit -> writer info
	commitToWriter := make(map[int64]writeResult)
	for _, result := range writeResults {
		if result.success {
			commitToWriter[result.commit] = result
		}
	}

	// Verify each commit in sequence is readable and correct
	for commitNum := int64(1); commitNum <= int64(len(writeResults)); commitNum++ {
		writerInfo, exists := commitToWriter[commitNum]
		if !exists {
			t.Errorf("commit %d: no writer found", commitNum)
			continue
		}

		// Retry reading until successful (with timeout)
		var state *ir.Node
		var err error
		for retry := 0; retry < 10; retry++ {
			state, err = storage.ReadStateAt(testPath, commitNum)
			if err == nil && state != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if err != nil {
			t.Errorf("failed to read state at commit %d after retries: %v", commitNum, err)
			continue
		}

		if state == nil {
			t.Errorf("state at commit %d should not be nil", commitNum)
			continue
		}

		if state.Type != ir.ObjectType {
			t.Errorf("expected object/map type at commit %d, got %v", commitNum, state.Type)
			continue
		}

		// Verify the state matches what the writer wrote
		var writerNode, valueNode *ir.Node
		for j, fieldName := range state.Fields {
			if fieldName.String == "writer" {
				writerNode = state.Values[j]
			}
			if fieldName.String == "value" {
				valueNode = state.Values[j]
			}
		}

		if writerNode == nil || writerNode.Int64 == nil {
			t.Errorf("commit %d: expected 'writer' field", commitNum)
			continue
		}
		if *writerNode.Int64 != int64(writerInfo.writerID) {
			t.Errorf("commit %d: expected writer %d, got %d", commitNum, writerInfo.writerID, *writerNode.Int64)
		}

		if valueNode == nil {
			t.Errorf("commit %d: expected 'value' field", commitNum)
			continue
		}
		if valueNode.String != writerInfo.value {
			t.Errorf("commit %d: expected value %q, got %q", commitNum, writerInfo.value, valueNode.String)
		}
	}
}

func verifyReadStates(t *testing.T, readStates []*ir.Node) {
	// Filter out Null states (reads that happened before any commits)
	validStates := make([]*ir.Node, 0)
	for _, state := range readStates {
		if state != nil && state.Type == ir.ObjectType {
			validStates = append(validStates, state)
		}
	}
	
	if len(validStates) == 0 {
		t.Error("no valid read states found (all reads happened before commits?)")
		return
	}
	
	for i, state := range validStates {
		if state == nil {
			t.Errorf("read state %d is nil", i)
			continue
		}
		if state.Type != ir.ObjectType {
			t.Errorf("read state %d: expected object/map type, got %v", i, state.Type)
			continue
		}
		// Verify structure is valid (has writer and value fields)
		hasWriter := false
		hasValue := false
		for _, fieldName := range state.Fields {
			if fieldName.String == "writer" {
				hasWriter = true
			}
			if fieldName.String == "value" {
				hasValue = true
			}
		}
		if !hasWriter || !hasValue {
			t.Errorf("read state %d: missing expected fields (writer: %v, value: %v)", i, hasWriter, hasValue)
		}
	}
}

func TestConcurrentWritesToSamePath(t *testing.T) {
	tmpDir := t.TempDir()
	// Use a very high divisor to effectively disable compaction
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

	const numWriters = 2
	const numReaders = 2
	const testPath = "/test/concurrent-path"

	// Run writers and readers concurrently
	var wg sync.WaitGroup
	var writeResults []writeResult
	var readStates []*ir.Node

	wg.Add(1)
	go func() {
		defer wg.Done()
		writeResults = runConcurrentWriters(t, storage, testPath, numWriters)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		readStates = runConcurrentReaders(t, storage, testPath, numReaders)
	}()

	wg.Wait()

	// After writers complete, all commits are done (Commit() is synchronous)
	// No need to wait for "quiescence" - commits are immediately visible after Commit() returns

	// Verifications
	verifyAllWritesSucceeded(t, writeResults)
	verifyCommitSequence(t, writeResults)
	verifyFinalState(t, storage, testPath, writeResults)
	verifyCommitStates(t, storage, testPath, writeResults)
	verifyReadStates(t, readStates)
}

func TestWriteThenReadCurrent(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Write: Create transaction and commit a patch
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	patcher := tx.NewPatcher()

	// Create a simple diff: set /test/path to {"key": "value"}
	diff := ir.FromMap(map[string]*ir.Node{
		"key": ir.FromString("value"),
	})
	patch := createTestPatch("/test/path", diff, nil)

	isLast, err := patcher.AddPatch(patch)
	if err != nil {
		t.Fatalf("failed to add patch: %v", err)
	}
	if !isLast {
		t.Fatal("single participant should be last")
	}

	result := patcher.Commit()
	if !result.Committed {
		t.Fatal("transaction should be committed")
	}
	if result.Commit == 0 {
		t.Fatal("commit number should be non-zero")
	}

	// Read: Read back the current state
	readState, err := storage.ReadCurrentState("/test/path")
	if err != nil {
		t.Fatalf("failed to read current state: %v", err)
	}

	// Verify: Check that the read state matches what we wrote
	if readState == nil {
		t.Fatal("read state should not be nil")
	}

	// Verify the map structure
	if readState.Type != ir.ObjectType {
		t.Errorf("expected object/map type, got %v", readState.Type)
	}

	// Verify the "key" field exists and has the correct value
	var keyNode *ir.Node
	for i, fieldName := range readState.Fields {
		if fieldName.String == "key" {
			keyNode = readState.Values[i]
			break
		}
	}
	if keyNode == nil {
		t.Fatal("expected 'key' field in read state")
	}
	if keyNode.Type != ir.StringType {
		t.Errorf("expected string type for 'key', got %v", keyNode.Type)
	}
	if keyNode.String != "value" {
		t.Errorf("expected 'key' value 'value', got %q", keyNode.String)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	const numPaths = 20
	paths := make([]string, numPaths)
	expectedValues := make(map[string]string)

	// Prepare paths and expected values
	for i := 0; i < numPaths; i++ {
		path := fmt.Sprintf("/test/path%d", i)
		paths[i] = path
		expectedValues[path] = fmt.Sprintf("value%d", i)
	}

	var wg sync.WaitGroup
	var readMu sync.Mutex
	readResults := make(map[string]*ir.Node)
	readErrors := make(map[string]error)
	readAttempts := make(map[string]int)

	// Launch N reader goroutines, each reading one path repeatedly until it gets a value
	for i := 0; i < numPaths; i++ {
		wg.Add(1)
		path := paths[i]
		go func(p string) {
			defer wg.Done()
			maxAttempts := 100
			attempt := 0

			for attempt < maxAttempts {
				attempt++
				readMu.Lock()
				readAttempts[p] = attempt
				readMu.Unlock()

				state, err := storage.ReadCurrentState(p)
				if err == nil && state != nil {
					// Check if we got a non-null value (path exists and has data)
					if state.Type == ir.ObjectType {
						// Check if it has the expected "key" field
						var keyNode *ir.Node
						for j, fieldName := range state.Fields {
							if fieldName.String == "key" {
								keyNode = state.Values[j]
								break
							}
						}
						if keyNode != nil && keyNode.String != "" {
							readMu.Lock()
							readResults[p] = state
							readMu.Unlock()
							return // Successfully read the value
						}
					}
				}

				// Small delay before retrying
				time.Sleep(10 * time.Millisecond)
			}

			// If we get here, we didn't successfully read
			readMu.Lock()
			if readResults[p] == nil {
				readErrors[p] = fmt.Errorf("failed to read path %s after %d attempts", p, maxAttempts)
			}
			readMu.Unlock()
		}(path)
	}

	// Launch 2 writer goroutines, each writing N/2 paths in parallel
	writer1Done := make(chan bool)
	writer2Done := make(chan bool)

	// Writer 1: writes paths 0 to N/2-1
	go func() {
		defer close(writer1Done)
		for i := 0; i < numPaths/2; i++ {
			path := paths[i]
			value := expectedValues[path]

			tx, err := storage.NewTx(1)
			if err != nil {
				t.Errorf("writer1: failed to create transaction for path %s: %v", path, err)
				continue
			}
			patcher := tx.NewPatcher()

			diff := ir.FromMap(map[string]*ir.Node{
				"key": ir.FromString(value),
			})
			patch := createTestPatch(path, diff, nil)

			isLast, err := patcher.AddPatch(patch)
			if err != nil {
				t.Errorf("writer1: failed to add patch for path %s: %v", path, err)
				continue
			}
			if !isLast {
				t.Errorf("writer1: single participant should be last for path %s", path)
				continue
			}

			result := patcher.Commit()
			if !result.Committed {
				t.Errorf("writer1: transaction should be committed for path %s", path)
				continue
			}
		}
	}()

	// Writer 2: writes paths N/2 to N-1
	go func() {
		defer close(writer2Done)
		for i := numPaths / 2; i < numPaths; i++ {
			path := paths[i]
			value := expectedValues[path]

			tx, err := storage.NewTx(1)
			if err != nil {
				t.Errorf("writer2: failed to create transaction for path %s: %v", path, err)
				continue
			}
			patcher := tx.NewPatcher()

			diff := ir.FromMap(map[string]*ir.Node{
				"key": ir.FromString(value),
			})
			patch := createTestPatch(path, diff, nil)

			isLast, err := patcher.AddPatch(patch)
			if err != nil {
				t.Errorf("writer2: failed to add patch for path %s: %v", path, err)
				continue
			}
			if !isLast {
				t.Errorf("writer2: single participant should be last for path %s", path)
				continue
			}

			result := patcher.Commit()
			if !result.Committed {
				t.Errorf("writer2: transaction should be committed for path %s", path)
				continue
			}
		}
	}()

	// Wait for writers to finish
	<-writer1Done
	<-writer2Done

	// Wait for all readers to finish (they should eventually see the values)
	wg.Wait()

	// Verify: Check all reads succeeded and values match
	if len(readErrors) > 0 {
		for path, err := range readErrors {
			t.Errorf("failed to read path %s: %v (attempts: %d)", path, err, readAttempts[path])
		}
	}

	if len(readResults) != numPaths {
		t.Errorf("expected %d read results, got %d", numPaths, len(readResults))
	}

	for path, expectedValue := range expectedValues {
		state, ok := readResults[path]
		if !ok {
			t.Errorf("missing read result for path %s (attempts: %d)", path, readAttempts[path])
			continue
		}

		if state == nil {
			t.Errorf("read state is nil for path %s", path)
			continue
		}

		if state.Type != ir.ObjectType {
			t.Errorf("expected object/map type for path %s, got %v", path, state.Type)
			continue
		}

		// Verify the "key" field exists and has the correct value
		var keyNode *ir.Node
		for i, fieldName := range state.Fields {
			if fieldName.String == "key" {
				keyNode = state.Values[i]
				break
			}
		}
		if keyNode == nil {
			t.Errorf("expected 'key' field in read state for path %s", path)
			continue
		}
		if keyNode.Type != ir.StringType {
			t.Errorf("expected string type for 'key' in path %s, got %v", path, keyNode.Type)
			continue
		}
		if keyNode.String != expectedValue {
			t.Errorf("expected 'key' value %q for path %s, got %q", expectedValue, path, keyNode.String)
		}
	}
}
