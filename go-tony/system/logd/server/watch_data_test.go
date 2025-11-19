package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestWatchData_StreamsExistingDiffs(t *testing.T) {
	// Set up temporary storage
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(s)

	// Write some diffs first
	processes := []struct {
		id    string
		pid   int64
		name  string
		state string
	}{
		{"proc-1", 1234, "nginx", "running"},
		{"proc-2", 5678, "apache", "stopped"},
	}

	for _, proc := range processes {
		writeRequestBody := fmt.Sprintf(`path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: %q
  pid: %d
  name: %q
  state: %q
`, proc.id, proc.pid, proc.name, proc.state)

		writeReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(writeRequestBody))
		writeReq.Header.Set("Content-Type", "application/x-tony")
		writeResp := httptest.NewRecorder()

		writeBody, err := api.ParseRequestBody(writeReq)
		if err != nil {
			t.Fatalf("failed to parse write request body: %v", err)
		}

		server.handlePatchData(writeResp, writeReq, writeBody)
		if writeResp.Code != http.StatusOK {
			t.Fatalf("expected status 200 for write, got %d: %s", writeResp.Code, writeResp.Body.String())
		}
	}

	// Now watch from the beginning
	watchRequestBody := `path: /proc/processes
match: null
meta:
  fromSeq: 1
`

	watchReq := httptest.NewRequest("WATCH", "/api/data", bytes.NewBufferString(watchRequestBody))
	watchReq.Header.Set("Content-Type", "application/x-tony")
	watchResp := httptest.NewRecorder()

	watchBody, err := api.ParseRequestBody(watchReq)
	if err != nil {
		t.Fatalf("failed to parse watch request body: %v", err)
	}

	// Set up context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	watchReq = watchReq.WithContext(ctx)

	// Run watch in goroutine since it blocks
	done := make(chan bool)
	go func() {
		server.handleWatchData(watchResp, watchReq, watchBody)
		done <- true
	}()

	// Wait a bit for streaming
	time.Sleep(200 * time.Millisecond)
	cancel() // Cancel context to stop watching
	<-done

	// Parse streamed documents
	body := watchResp.Body.String()
	if body == "" {
		t.Fatal("expected streamed content, got empty")
	}

	// Split by --- separator
	docs := strings.Split(body, "---")
	if len(docs) == 0 {
		t.Fatal("expected at least one document")
	}

	// Verify we got both diffs
	if len(docs) < 2 {
		t.Fatalf("expected at least 2 documents, got %d", len(docs))
	}

	// Parse first document
	doc1, err := parse.Parse([]byte(strings.TrimSpace(docs[0])))
	if err != nil {
		t.Fatalf("failed to parse first document: %v", err)
	}

	meta1 := ir.Get(doc1, "meta")
	if meta1 == nil {
		t.Fatal("expected meta in first document")
	}

	seq1 := ir.Get(meta1, "seq")
	if seq1 == nil || seq1.Int64 == nil {
		t.Fatal("expected seq in first document meta")
	}

	// Parse second document
	doc2, err := parse.Parse([]byte(strings.TrimSpace(docs[1])))
	if err != nil {
		t.Fatalf("failed to parse second document: %v", err)
	}

	meta2 := ir.Get(doc2, "meta")
	if meta2 == nil {
		t.Fatal("expected meta in second document")
	}

	seq2 := ir.Get(meta2, "seq")
	if seq2 == nil || seq2.Int64 == nil {
		t.Fatal("expected seq in second document meta")
	}

	// Verify sequence numbers are in order
	if *seq1.Int64 >= *seq2.Int64 {
		t.Errorf("expected seq1 (%d) < seq2 (%d)", *seq1.Int64, *seq2.Int64)
	}
}

func TestWatchData_RealTimeWatching(t *testing.T) {
	// Set up temporary storage
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(s)

	// Write initial diff
	writeRequestBody := `path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-1"
  pid: 1234
  name: "nginx"
  state: "running"
`

	writeReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(writeRequestBody))
	writeReq.Header.Set("Content-Type", "application/x-tony")
	writeResp := httptest.NewRecorder()

	writeBody, err := api.ParseRequestBody(writeReq)
	if err != nil {
		t.Fatalf("failed to parse write request body: %v", err)
	}

	server.handlePatchData(writeResp, writeReq, writeBody)
	if writeResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for write, got %d: %s", writeResp.Code, writeResp.Body.String())
	}

	// Get the seq from the write response to know where to start watching
	writeDoc, err := parse.Parse(writeResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse write response: %v", err)
	}
	writeMeta := ir.Get(writeDoc, "meta")
	writeSeq := ir.Get(writeMeta, "seq")
	if writeSeq == nil || writeSeq.Int64 == nil {
		t.Fatal("expected seq in write response")
	}
	startSeq := *writeSeq.Int64

	// Set up watch from current state (no fromSeq means watch from now)
	watchRequestBody := `path: /proc/processes
match: null
`

	watchReq := httptest.NewRequest("WATCH", "/api/data", bytes.NewBufferString(watchRequestBody))
	watchReq.Header.Set("Content-Type", "application/x-tony")

	watchBody, err := api.ParseRequestBody(watchReq)
	if err != nil {
		t.Fatalf("failed to parse watch request body: %v", err)
	}

	// Create a response recorder that supports flushing
	watchResp := httptest.NewRecorder()

	// Set up context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	watchReq = watchReq.WithContext(ctx)

	// Run watch in goroutine
	done := make(chan bool)
	go func() {
		server.handleWatchData(watchResp, watchReq, watchBody)
		done <- true
	}()

	// Wait a bit, then write a new diff
	time.Sleep(300 * time.Millisecond)

	// Write another diff while watching
	writeRequestBody2 := `path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-2"
  pid: 5678
  name: "apache"
  state: "stopped"
`

	writeReq2 := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(writeRequestBody2))
	writeReq2.Header.Set("Content-Type", "application/x-tony")
	writeResp2 := httptest.NewRecorder()

	writeBody2, err := api.ParseRequestBody(writeReq2)
	if err != nil {
		t.Fatalf("failed to parse write request body: %v", err)
	}

	server.handlePatchData(writeResp2, writeReq2, writeBody2)
	if writeResp2.Code != http.StatusOK {
		t.Fatalf("expected status 200 for write, got %d: %s", writeResp2.Code, writeResp2.Body.String())
	}

	// Wait for the watch to pick up the new diff
	time.Sleep(300 * time.Millisecond)

	// Cancel context to stop watching
	cancel()
	<-done

	// Parse streamed documents
	body := watchResp.Body.String()
	if body == "" {
		t.Fatal("expected streamed content, got empty")
	}

	// Split by --- separator
	docs := strings.Split(body, "---")
	// Filter out empty strings
	var nonEmptyDocs []string
	for _, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed != "" {
			nonEmptyDocs = append(nonEmptyDocs, trimmed)
		}
	}

	// We should have at least the new diff (proc-2)
	// The initial state might or might not be included depending on timing
	foundProc2 := false
	seqs := make([]int64, 0)

	for _, docStr := range nonEmptyDocs {
		doc, err := parse.Parse([]byte(docStr))
		if err != nil {
			t.Logf("failed to parse document: %v\n%s", err, docStr)
			continue
		}

		meta := ir.Get(doc, "meta")
		if meta == nil {
			continue
		}

		seq := ir.Get(meta, "seq")
		if seq != nil && seq.Int64 != nil {
			seqs = append(seqs, *seq.Int64)
		}

		diff := ir.Get(doc, "diff")
		if diff != nil && diff.Type == ir.ArrayType {
			for _, val := range diff.Values {
				idNode := ir.Get(val, "id")
				if idNode != nil && idNode.String == "proc-2" {
					foundProc2 = true
				}
			}
		}
	}

	if !foundProc2 {
		t.Error("expected to find proc-2 in streamed diffs")
		t.Logf("streamed body: %s", body)
	}

	// Verify sequence numbers are in order (monotonic)
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("sequence numbers not in order: %d <= %d", seqs[i], seqs[i-1])
		}
	}

	// Verify all seqs are >= startSeq
	for _, seq := range seqs {
		if seq < startSeq {
			t.Errorf("found seq %d < startSeq %d", seq, startSeq)
		}
	}
}

func TestWatchData_WithToSeq(t *testing.T) {
	// Set up temporary storage
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(s)

	// Write 3 diffs
	for i := 1; i <= 3; i++ {
		writeRequestBody := fmt.Sprintf(`path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-%d"
  pid: %d
  name: "process-%d"
  state: "running"
`, i, 1000+i, i)

		writeReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(writeRequestBody))
		writeReq.Header.Set("Content-Type", "application/x-tony")
		writeResp := httptest.NewRecorder()

		writeBody, err := api.ParseRequestBody(writeReq)
		if err != nil {
			t.Fatalf("failed to parse write request body: %v", err)
		}

		server.handlePatchData(writeResp, writeReq, writeBody)
		if writeResp.Code != http.StatusOK {
			t.Fatalf("expected status 200 for write, got %d", writeResp.Code)
		}
	}

	// Watch with toSeq=2 (should only stream first 2 diffs)
	watchRequestBody := `path: /proc/processes
match: null
meta:
  fromSeq: 1
  toSeq: 2
`

	watchReq := httptest.NewRequest("WATCH", "/api/data", bytes.NewBufferString(watchRequestBody))
	watchReq.Header.Set("Content-Type", "application/x-tony")
	watchResp := httptest.NewRecorder()

	watchBody, err := api.ParseRequestBody(watchReq)
	if err != nil {
		t.Fatalf("failed to parse watch request body: %v", err)
	}

	// Set up context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	watchReq = watchReq.WithContext(ctx)

	// Run watch
	done := make(chan bool)
	go func() {
		server.handleWatchData(watchResp, watchReq, watchBody)
		done <- true
	}()

	// Wait for completion
	select {
	case <-done:
		// Good, watch completed
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not complete within timeout")
	}

	// Parse streamed documents
	body := watchResp.Body.String()
	docs := strings.Split(body, "---")
	var nonEmptyDocs []string
	for _, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed != "" {
			nonEmptyDocs = append(nonEmptyDocs, trimmed)
		}
	}

	// Should have exactly 2 documents (seq 1 and 2)
	if len(nonEmptyDocs) != 2 {
		t.Errorf("expected 2 documents with toSeq=2, got %d", len(nonEmptyDocs))
	}

	// Verify sequence numbers
	seqs := make([]int64, 0)
	for _, docStr := range nonEmptyDocs {
		doc, err := parse.Parse([]byte(docStr))
		if err != nil {
			continue
		}
		meta := ir.Get(doc, "meta")
		if meta == nil {
			continue
		}
		seq := ir.Get(meta, "seq")
		if seq != nil && seq.Int64 != nil {
			seqs = append(seqs, *seq.Int64)
		}
	}

	if len(seqs) != 2 {
		t.Errorf("expected 2 sequence numbers, got %d", len(seqs))
	}

	if len(seqs) >= 2 {
		if seqs[0] != 1 || seqs[1] != 2 {
			t.Errorf("expected seqs [1, 2], got %v", seqs)
		}
		// Verify no seq > 2
		for _, seq := range seqs {
			if seq > 2 {
				t.Errorf("found seq %d > toSeq 2", seq)
			}
		}
	}
}

func TestWatchData_StateReconstruction(t *testing.T) {
	// Test that streaming diffs in order allows correct state reconstruction
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(s)

	// Write multiple diffs
	diffs := []string{
		`path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-1"
  pid: 1234
  name: "nginx"
  state: "running"
`,
		`path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-2"
  pid: 5678
  name: "apache"
  state: "stopped"
`,
		`path: /proc/processes
match: null
patch: !key(id)
- id: "proc-1"
  state: !replace
    from: "running"
    to: "killed"
`,
	}

	for _, diffBody := range diffs {
		writeReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(diffBody))
		writeReq.Header.Set("Content-Type", "application/x-tony")
		writeResp := httptest.NewRecorder()

		writeBody, err := api.ParseRequestBody(writeReq)
		if err != nil {
			t.Fatalf("failed to parse write request body: %v", err)
		}

		server.handlePatchData(writeResp, writeReq, writeBody)
		if writeResp.Code != http.StatusOK {
			t.Fatalf("expected status 200 for write, got %d", writeResp.Code)
		}
	}

	// Watch all diffs
	watchRequestBody := `path: /proc/processes
match: null
meta:
  fromSeq: 1
`

	watchReq := httptest.NewRequest("WATCH", "/api/data", bytes.NewBufferString(watchRequestBody))
	watchReq.Header.Set("Content-Type", "application/x-tony")
	watchResp := httptest.NewRecorder()

	watchBody, err := api.ParseRequestBody(watchReq)
	if err != nil {
		t.Fatalf("failed to parse watch request body: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	watchReq = watchReq.WithContext(ctx)

	done := make(chan bool)
	go func() {
		server.handleWatchData(watchResp, watchReq, watchBody)
		done <- true
	}()

	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done

	// Parse all streamed diffs
	body := watchResp.Body.String()
	docs := strings.Split(body, "---")
	var diffsStreamed []*ir.Node
	for _, docStr := range docs {
		trimmed := strings.TrimSpace(docStr)
		if trimmed == "" {
			continue
		}
		doc, err := parse.Parse([]byte(trimmed))
		if err != nil {
			continue
		}
		diff := ir.Get(doc, "diff")
		if diff != nil {
			diffsStreamed = append(diffsStreamed, diff)
		}
	}

	// Verify we got all 3 diffs
	if len(diffsStreamed) < 3 {
		t.Errorf("expected at least 3 diffs, got %d", len(diffsStreamed))
	}

	// Verify sequence: first two should be inserts, third should be replace
	// This ensures diffs are streamed in commitCount order
	if len(diffsStreamed) >= 3 {
		// First diff should insert proc-1
		diff1 := diffsStreamed[0]
		foundProc1Insert := false
		if diff1.Type == ir.ArrayType {
			for _, val := range diff1.Values {
				idNode := ir.Get(val, "id")
				if idNode != nil && idNode.String == "proc-1" {
					foundProc1Insert = true
					break
				}
			}
		}
		if !foundProc1Insert {
			t.Error("expected first diff to insert proc-1")
		}

		// Third diff should update proc-1 state
		diff3 := diffsStreamed[2]
		foundProc1Update := false
		if diff3.Type == ir.ArrayType {
			for _, val := range diff3.Values {
				idNode := ir.Get(val, "id")
				if idNode != nil && idNode.String == "proc-1" {
					stateNode := ir.Get(val, "state")
					if stateNode != nil {
						// Check if it's a replace operation
						if stateNode.Tag == "!replace" {
							foundProc1Update = true
						}
					}
				}
			}
		}
		if !foundProc1Update {
			t.Error("expected third diff to update proc-1 state")
		}
	}
}

func TestWatchData_TransactionWrite(t *testing.T) {
	// Test that watching a path shows the results of a transaction write
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(s)

	// Step 1: Create a transaction with 2 participants
	createTxRequestBody := `path: /api/transactions
match: null
patch: !key(id)
- !insert
  participantCount: 2
`

	createTxReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(createTxRequestBody))
	createTxReq.Header.Set("Content-Type", "application/x-tony")
	createTxResp := httptest.NewRecorder()

	createTxBody, err := api.ParseRequestBody(createTxReq)
	if err != nil {
		t.Fatalf("failed to parse create transaction request body: %v", err)
	}

	server.handlePatchTransaction(createTxResp, createTxReq, createTxBody)
	if createTxResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for create transaction, got %d: %s", createTxResp.Code, createTxResp.Body.String())
	}

	// Extract transaction ID
	createTxDoc, err := parse.Parse(createTxResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse create transaction response: %v", err)
	}

	patchNode := ir.Get(createTxDoc, "patch")
	if patchNode == nil {
		t.Fatal("expected patch in create transaction response")
	}

	transactionIDNode := ir.Get(patchNode.Values[0], "transactionId")
	if transactionIDNode == nil || transactionIDNode.String == "" {
		t.Fatal("expected transactionId in create response")
	}
	transactionID := transactionIDNode.String

	// Step 2: Start watching before the transaction commits
	watchRequestBody := `path: /proc/processes
match: null
`

	watchReq := httptest.NewRequest("WATCH", "/api/data", bytes.NewBufferString(watchRequestBody))
	watchReq.Header.Set("Content-Type", "application/x-tony")
	watchResp := httptest.NewRecorder()

	watchBody, err := api.ParseRequestBody(watchReq)
	if err != nil {
		t.Fatalf("failed to parse watch request body: %v", err)
	}

	// Set up context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	watchReq = watchReq.WithContext(ctx)

	// Run watch in goroutine
	done := make(chan bool)
	go func() {
		server.handleWatchData(watchResp, watchReq, watchBody)
		done <- true
	}()

	// Wait a bit for watch to start
	time.Sleep(200 * time.Millisecond)

	// Step 3: Write both diffs concurrently with the transaction
	write1RequestBody := fmt.Sprintf(`path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-1"
  pid: 1234
  name: "nginx"
meta:
  tx-id: %q
`, transactionID)

	write2RequestBody := fmt.Sprintf(`path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-2"
  pid: 5678
  name: "apache"
meta:
  tx-id: %q
`, transactionID)

	write1Req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(write1RequestBody))
	write1Req.Header.Set("Content-Type", "application/x-tony")
	write1Resp := httptest.NewRecorder()

	write2Req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(write2RequestBody))
	write2Req.Header.Set("Content-Type", "application/x-tony")
	write2Resp := httptest.NewRecorder()

	write1Body, err := api.ParseRequestBody(write1Req)
	if err != nil {
		t.Fatalf("failed to parse write1 request body: %v", err)
	}

	write2Body, err := api.ParseRequestBody(write2Req)
	if err != nil {
		t.Fatalf("failed to parse write2 request body: %v", err)
	}

	// Run both writes concurrently - both will block until transaction commits
	writeDone := make(chan bool, 2)

	go func() {
		server.handlePatchData(write1Resp, write1Req, write1Body)
		writeDone <- true
	}()

	go func() {
		server.handlePatchData(write2Resp, write2Req, write2Body)
		writeDone <- true
	}()

	// Wait for both writes to complete
	<-writeDone
	<-writeDone

	if write1Resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for write1, got %d: %s", write1Resp.Code, write1Resp.Body.String())
	}

	if write2Resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for write2, got %d: %s", write2Resp.Code, write2Resp.Body.String())
	}

	// Verify both writes have the same commit count
	write1Doc, err := parse.Parse(write1Resp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse write1 response: %v", err)
	}

	write2Doc, err := parse.Parse(write2Resp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse write2 response: %v", err)
	}

	meta1 := ir.Get(write1Doc, "meta")
	meta2 := ir.Get(write2Doc, "meta")
	if meta1 == nil || meta2 == nil {
		t.Fatal("expected meta in both write responses")
	}

	seq1 := ir.Get(meta1, "seq")
	seq2 := ir.Get(meta2, "seq")
	if seq1 == nil || seq1.Int64 == nil || seq2 == nil || seq2.Int64 == nil {
		t.Fatal("expected seq in both write responses")
	}

	if *seq1.Int64 != *seq2.Int64 {
		t.Errorf("expected both writes to have same commit count, got %d and %d", *seq1.Int64, *seq2.Int64)
	}

	// Wait for watch to pick up the new diffs
	time.Sleep(500 * time.Millisecond)

	// Cancel context to stop watching
	cancel()
	<-done

	// Parse streamed documents
	body := watchResp.Body.String()
	if body == "" {
		t.Fatal("expected streamed content, got empty")
	}

	// Split by --- separator
	docs := strings.Split(body, "---")
	var nonEmptyDocs []string
	for _, doc := range docs {
		trimmed := strings.TrimSpace(doc)
		if trimmed != "" {
			nonEmptyDocs = append(nonEmptyDocs, trimmed)
		}
	}

	// We should have both diffs from the transaction
	if len(nonEmptyDocs) < 2 {
		t.Fatalf("expected at least 2 documents from transaction, got %d", len(nonEmptyDocs))
	}

	// Find the documents with the transaction commit count
	expectedCommitCount := *seq1.Int64
	foundProc1 := false
	foundProc2 := false
	seqs := make([]int64, 0)

	for _, docStr := range nonEmptyDocs {
		doc, err := parse.Parse([]byte(docStr))
		if err != nil {
			t.Logf("failed to parse document: %v\n%s", err, docStr)
			continue
		}

		meta := ir.Get(doc, "meta")
		if meta == nil {
			continue
		}

		seq := ir.Get(meta, "seq")
		if seq != nil && seq.Int64 != nil {
			seqs = append(seqs, *seq.Int64)
		}

		diff := ir.Get(doc, "diff")
		if diff != nil && diff.Type == ir.ArrayType {
			for _, val := range diff.Values {
				idNode := ir.Get(val, "id")
				if idNode != nil {
					if idNode.String == "proc-1" {
						foundProc1 = true
						// Verify this diff has the expected commit count
						if seq != nil && seq.Int64 != nil && *seq.Int64 == expectedCommitCount {
							// Good, this is from our transaction
						}
					}
					if idNode.String == "proc-2" {
						foundProc2 = true
						// Verify this diff has the expected commit count
						if seq != nil && seq.Int64 != nil && *seq.Int64 == expectedCommitCount {
							// Good, this is from our transaction
						}
					}
				}
			}
		}
	}

	if !foundProc1 {
		t.Error("expected to find proc-1 in streamed diffs")
	}

	if !foundProc2 {
		t.Error("expected to find proc-2 in streamed diffs")
	}

	// Verify sequence numbers are in order (non-decreasing)
	// Note: diffs from the same transaction can have the same commit count
	for i := 1; i < len(seqs); i++ {
		if seqs[i] < seqs[i-1] {
			t.Errorf("sequence numbers not in order: %d < %d", seqs[i], seqs[i-1])
		}
	}

	// Verify both transaction diffs have the same commit count
	transactionSeqs := make([]int64, 0)
	for _, docStr := range nonEmptyDocs {
		doc, err := parse.Parse([]byte(docStr))
		if err != nil {
			continue
		}

		meta := ir.Get(doc, "meta")
		if meta == nil {
			continue
		}

		seq := ir.Get(meta, "seq")
		if seq == nil || seq.Int64 == nil {
			continue
		}

		diff := ir.Get(doc, "diff")
		if diff != nil && diff.Type == ir.ArrayType {
			for _, val := range diff.Values {
				idNode := ir.Get(val, "id")
				if idNode != nil && (idNode.String == "proc-1" || idNode.String == "proc-2") {
					transactionSeqs = append(transactionSeqs, *seq.Int64)
					break
				}
			}
		}
	}

	if len(transactionSeqs) != 2 {
		t.Errorf("expected 2 transaction diffs, found %d", len(transactionSeqs))
	} else {
		if transactionSeqs[0] != expectedCommitCount || transactionSeqs[1] != expectedCommitCount {
			t.Errorf("expected both transaction diffs to have commit count %d, got %v", expectedCommitCount, transactionSeqs)
		}
	}
}
