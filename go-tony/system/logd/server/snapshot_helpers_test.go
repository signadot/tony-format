package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestReconstructState_WithSnapshot(t *testing.T) {
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

	// Write some diffs
	pathStr := "/proc/processes"
	processes := []struct {
		id    string
		pid   int64
		name  string
		state string
	}{
		{"proc-1", 1234, "nginx", "running"},
		{"proc-2", 5678, "apache", "stopped"},
		{"proc-3", 9012, "mysql", "running"},
	}

	for _, proc := range processes {
		writeRequestBody := fmt.Sprintf(`path: %s
match: null
patch: !key(id)
- !insert
  id: %q
  pid: %d
  name: %q
  state: %q
`, pathStr, proc.id, proc.pid, proc.name, proc.state)

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

	// Create snapshot at commit count 2 (after first 2 processes)
	targetCommitCount := int64(2)
	if err := server.createSnapshot(pathStr, targetCommitCount); err != nil {
		t.Fatalf("failed to create snapshot: %v", err)
	}

	// Write more diffs after snapshot
	writeRequestBody := fmt.Sprintf(`path: %s
match: null
patch: !key(id)
- !insert
  id: "proc-4"
  pid: 3456
  name: "redis"
  state: "running"
`, pathStr)

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

	// Reconstruct state at latest (should use snapshot)
	state, commitCount, err := server.reconstructState(pathStr, nil)
	if err != nil {
		t.Fatalf("failed to reconstruct state: %v", err)
	}

	if commitCount != 4 {
		t.Errorf("expected commit count 4, got %d", commitCount)
	}

	// Verify we have all 4 processes
	if state == nil || state.Type != ir.ArrayType {
		t.Fatalf("expected array state, got %v", state)
	}

	procCount := 0
	for _, val := range state.Values {
		idNode := ir.Get(val, "id")
		if idNode != nil {
			procCount++
		}
	}

	if procCount != 4 {
		t.Errorf("expected 4 processes, got %d", procCount)
	}
}

func TestReconstructState_WithoutSnapshot(t *testing.T) {
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

	// Write some diffs
	pathStr := "/proc/processes"
	writeRequestBody := fmt.Sprintf(`path: %s
match: null
patch: !key(id)
- !insert
  id: "proc-1"
  pid: 1234
  name: "nginx"
  state: "running"
`, pathStr)

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

	// Reconstruct state without snapshot (should fall back to diff replay)
	state, commitCount, err := server.reconstructState(pathStr, nil)
	if err != nil {
		t.Fatalf("failed to reconstruct state: %v", err)
	}

	if commitCount != 1 {
		t.Errorf("expected commit count 1, got %d", commitCount)
	}

	// Verify we have the process
	if state == nil || state.Type != ir.ArrayType {
		t.Fatalf("expected array state, got %v", state)
	}

	foundProc1 := false
	for _, val := range state.Values {
		idNode := ir.Get(val, "id")
		if idNode != nil && idNode.String == "proc-1" {
			foundProc1 = true
			break
		}
	}

	if !foundProc1 {
		t.Error("expected to find proc-1 in state")
	}
}

func TestCreateSnapshot(t *testing.T) {
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

	// Write some diffs
	pathStr := "/proc/processes"
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
		writeRequestBody := fmt.Sprintf(`path: %s
match: null
patch: !key(id)
- !insert
  id: %q
  pid: %d
  name: %q
  state: %q
`, pathStr, proc.id, proc.pid, proc.name, proc.state)

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

	// Create snapshot at commit count 2
	commitCount := int64(2)
	if err := server.createSnapshot(pathStr, commitCount); err != nil {
		t.Fatalf("failed to create snapshot: %v", err)
	}

	// Verify snapshot exists
	snapshot, err := s.ReadSnapshot(pathStr, commitCount)
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}

	if snapshot.CommitCount != commitCount {
		t.Errorf("expected commit count %d, got %d", commitCount, snapshot.CommitCount)
	}

	// Verify snapshot state contains both processes
	if snapshot.State == nil || snapshot.State.Type != ir.ArrayType {
		t.Fatalf("expected array state, got %v", snapshot.State)
	}

	procCount := 0
	for _, val := range snapshot.State.Values {
		idNode := ir.Get(val, "id")
		if idNode != nil {
			procCount++
		}
	}

	if procCount != 2 {
		t.Errorf("expected 2 processes in snapshot, got %d", procCount)
	}
}
