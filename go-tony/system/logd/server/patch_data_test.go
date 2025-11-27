package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestHandlePatchData_Success(t *testing.T) {
	// Set up temporary storage
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	//t.Logf("logd in %s", tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(&Config{Storage: s})

	// Create request body
	requestBody := `body:
  path: /proc/processes
  patch: !key(id)
  - !insert
    id: "proc-1"
    pid: 1234
    name: "nginx"
    state: "running"
`

	req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/x-tony")
	w := httptest.NewRecorder()

	// Parse body manually to call handler directly
	patchReq := &api.Patch{}
	if err := patchReq.FromTony([]byte(requestBody)); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	server.handlePatchData(w, req, patchReq)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
		return
	}

	if w.Header().Get("Content-Type") != "application/x-tony" {
		t.Errorf("expected Content-Type application/x-tony, got %s", w.Header().Get("Content-Type"))
	}

	// Parse response
	doc, err := parse.Parse(w.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Verify response structure
	if doc.Type != ir.ObjectType {
		t.Fatalf("expected object response, got %v", doc.Type)
	}

	// Check body.path field
	bodyNode := ir.Get(doc, "body")
	if bodyNode == nil {
		t.Fatal("expected body field in response")
	}
	pathNode := ir.Get(bodyNode, "path")
	if pathNode == nil || pathNode.String != "/proc/processes" {
		t.Errorf("expected path /proc/processes, got %v", pathNode)
	}

	// Check body.patch field contains the diff
	patchNode := ir.Get(bodyNode, "patch")
	if patchNode == nil {
		t.Fatal("expected patch field in response")
	}

	// Check meta field
	metaNode := ir.Get(doc, "meta")
	if metaNode == nil || metaNode.Type != ir.ObjectType {
		t.Fatal("expected meta field in response")
	}

	seqNode := ir.Get(metaNode, "seq")
	if seqNode == nil || seqNode.Type != ir.NumberType {
		t.Error("expected seq in meta")
	} else if seqNode.Int64 == nil || *seqNode.Int64 != 1 {
		t.Errorf("expected seq=1, got %v", seqNode.Int64)
	}

	whenNode := ir.Get(metaNode, "when")
	if whenNode == nil || whenNode.Type != ir.StringType || whenNode.String == "" {
		t.Error("expected when in meta")
	}

	// Verify diff file was written
	diffs, err := s.ListDiffs("/proc/processes")
	if err != nil {
		t.Fatalf("failed to list diffs: %v", err)
	}
	if len(diffs) != 1 {
		t.Errorf("expected 1 diff file, got %d", len(diffs))
	}
}

func TestHandlePatchData_InvalidPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(&Config{Storage: s})

	// Test missing path
	requestBody := `body:
  patch: !key(id)
  - !insert
    id: "proc-1"
`

	req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/x-tony")
	w := httptest.NewRecorder()

	patchReq := &api.Patch{}
	if err := patchReq.FromTony([]byte(requestBody)); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	server.handlePatchData(w, req, patchReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	// Test invalid path (doesn't start with /)
	requestBody = `body:
  path: proc/processes
  patch: !key(id)
  - !insert
    id: "proc-1"
`

	req = httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/x-tony")
	w = httptest.NewRecorder()

	patchReq = &api.Patch{}
	if err := patchReq.FromTony([]byte(requestBody)); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	server.handlePatchData(w, req, patchReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchData_MissingPatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(&Config{Storage: s})

	requestBody := `body:
  path: /proc/processes
`

	req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/x-tony")
	w := httptest.NewRecorder()

	patchReq := &api.Patch{}
	if err := patchReq.FromTony([]byte(requestBody)); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	server.handlePatchData(w, req, patchReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePatchData_TransactionWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(&Config{Storage: s})

	// Step 1: Manually create a transaction with 2 participants
	transactionID := "tx-1-2"
	state := storage.NewTransactionState(transactionID, 2)
	if err := s.WriteTransactionState(state); err != nil {
		t.Fatalf("failed to write transaction state: %v", err)
	}

	// Step 2 & 3: Write both diffs concurrently (both will block until transaction commits)
	write1RequestBody := fmt.Sprintf(`meta:
  tx: %q
body:
  path: /proc/processes
  patch: !key(id)
  - !insert
    id: "proc-1"
    pid: 1234
    name: "nginx"
`, transactionID)

	write2RequestBody := fmt.Sprintf(`meta:
  tx: %q
body:
  path: /users
  patch: !key(id)
  - !insert
    id: "user-1"
    name: "Alice"
`, transactionID)

	write1Req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(write1RequestBody))
	write1Req.Header.Set("Content-Type", "application/x-tony")
	write1Resp := httptest.NewRecorder()

	write2Req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(write2RequestBody))
	write2Req.Header.Set("Content-Type", "application/x-tony")
	write2Resp := httptest.NewRecorder()

	write1Patch := &api.Patch{}
	if err := write1Patch.FromTony([]byte(write1RequestBody)); err != nil {
		t.Fatalf("failed to parse write1 request body: %v", err)
	}

	write2Patch := &api.Patch{}
	if err := write2Patch.FromTony([]byte(write2RequestBody)); err != nil {
		t.Fatalf("failed to parse write2 request body: %v", err)
	}

	// Run both writes concurrently - both will block until transaction commits
	done := make(chan bool, 2)

	go func() {
		server.handlePatchData(write1Resp, write1Req, write1Patch)
		done <- true
	}()

	go func() {
		server.handlePatchData(write2Resp, write2Req, write2Patch)
		done <- true
	}()

	// Wait for both writes to complete
	<-done
	<-done

	if write1Resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for write1, got %d: %s", write1Resp.Code, write1Resp.Body.String())
	}

	if write2Resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for write2, got %d: %s", write2Resp.Code, write2Resp.Body.String())
	}

	// Verify both responses indicate committed status with commit count
	write1Doc, err := parse.Parse(write1Resp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse write1 response: %v", err)
	}

	write2Doc, err := parse.Parse(write2Resp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse write2 response: %v", err)
	}

	metaNode1 := ir.Get(write1Doc, "meta")
	if metaNode1 == nil {
		t.Fatal("expected meta field in write1 response")
	}

	metaNode2 := ir.Get(write2Doc, "meta")
	if metaNode2 == nil {
		t.Fatal("expected meta field in write2 response")
	}

	seqNode1 := ir.Get(metaNode1, "seq")
	if seqNode1 == nil || seqNode1.Type != ir.NumberType {
		t.Error("expected seq in write1 meta after commit")
	} else if seqNode1.Int64 == nil || *seqNode1.Int64 <= 0 {
		t.Errorf("expected positive seq in write1, got %v", seqNode1.Int64)
	}

	seqNode2 := ir.Get(metaNode2, "seq")
	if seqNode2 == nil || seqNode2.Type != ir.NumberType {
		t.Error("expected seq in write2 meta after commit")
	} else if seqNode2.Int64 == nil || *seqNode2.Int64 <= 0 {
		t.Errorf("expected positive seq in write2, got %v", seqNode2.Int64)
	}

	// Both should have the same commit count
	if seqNode1.Int64 != nil && seqNode2.Int64 != nil && *seqNode1.Int64 != *seqNode2.Int64 {
		t.Errorf("expected both writes to have same commit count, got %d and %d", *seqNode1.Int64, *seqNode2.Int64)
	}

	// Verify both diffs are now committed
	diffs1, err := s.ListDiffs("/proc/processes")
	if err != nil {
		t.Fatalf("failed to list diffs: %v", err)
	}
	if len(diffs1) != 1 {
		t.Errorf("expected 1 committed diff for /proc/processes, got %d", len(diffs1))
	}

	diffs2, err := s.ListDiffs("/users")
	if err != nil {
		t.Fatalf("failed to list diffs: %v", err)
	}
	if len(diffs2) != 1 {
		t.Errorf("expected 1 committed diff for /users, got %d", len(diffs2))
	}

	// Verify transaction state is committed
	state, err = s.ReadTransactionState(transactionID)
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}
	if state.Status != "committed" {
		t.Errorf("expected transaction status=committed, got %s", state.Status)
	}
	if state.ParticipantsReceived != 2 {
		t.Errorf("expected 2 participants received, got %d", state.ParticipantsReceived)
	}
}

func TestHandlePatchData_TransactionWrite_Errors(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(&Config{Storage: s})

	// Test: Transaction not found
	requestBody := `meta:
  tx: "tx-99999-2"
body:
  path: /proc/processes
  patch: !key(id)
  - !insert
    id: "proc-1"
`

	req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/x-tony")
	w := httptest.NewRecorder()

	patchReq := &api.Patch{}
	if err := patchReq.FromTony([]byte(requestBody)); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	server.handlePatchData(w, req, patchReq)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for non-existent transaction, got %d: %s", w.Code, w.Body.String())
	}
}
