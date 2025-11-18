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

func TestRoundTrip_PatchThenMatch(t *testing.T) {
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

	// Step 1: Write a diff using PATCH
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

	// Parse write response to get seq
	writeDoc, err := parse.Parse(writeResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse write response: %v", err)
	}

	writeMeta := ir.Get(writeDoc, "meta")
	if writeMeta == nil {
		t.Fatal("expected meta in write response")
	}

	writeSeq := ir.Get(writeMeta, "seq")
	if writeSeq == nil || writeSeq.Int64 == nil {
		t.Fatal("expected seq in write response meta")
	}
	expectedSeq := *writeSeq.Int64

	// Step 2: Read it back using MATCH
	readRequestBody := `path: /proc/processes
match: null
`

	readReq := httptest.NewRequest("MATCH", "/api/data", bytes.NewBufferString(readRequestBody))
	readReq.Header.Set("Content-Type", "application/x-tony")
	readResp := httptest.NewRecorder()

	readBody, err := api.ParseRequestBody(readReq)
	if err != nil {
		t.Fatalf("failed to parse read request body: %v", err)
	}

	server.handleMatchData(readResp, readReq, readBody)

	if readResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for read, got %d: %s", readResp.Code, readResp.Body.String())
	}

	// Parse read response
	readDoc, err := parse.Parse(readResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse read response: %v", err)
	}

	// Verify response structure
	if readDoc.Type != ir.ObjectType {
		t.Fatalf("expected object response, got %v", readDoc.Type)
	}

	// Check path
	pathNode := ir.Get(readDoc, "path")
	if pathNode == nil || pathNode.String != "/proc/processes" {
		t.Errorf("expected path /proc/processes, got %v", pathNode)
	}

	// Check meta.seq matches what we wrote
	readMeta := ir.Get(readDoc, "meta")
	if readMeta == nil {
		t.Fatal("expected meta in read response")
	}

	readSeq := ir.Get(readMeta, "seq")
	if readSeq == nil || readSeq.Int64 == nil {
		t.Fatal("expected seq in read response meta")
	}
	if *readSeq.Int64 != expectedSeq {
		t.Errorf("expected seq %d, got %d", expectedSeq, *readSeq.Int64)
	}

	// Check patch field contains the reconstructed state
	readPatch := ir.Get(readDoc, "patch")
	if readPatch == nil {
		t.Fatal("expected patch field in read response")
	}

	// Verify the state matches what we wrote
	// Since we used !key(id) in the diff, the result should always be an array
	if readPatch.Type != ir.ArrayType {
		t.Fatalf("expected array type (from !key(id) diff), got %v (tag: %q)", readPatch.Type, readPatch.Tag)
	}

	// Look for proc-1 in the array
	var procIdNode *ir.Node
	for _, val := range readPatch.Values {
		idNode := ir.Get(val, "id")
		if idNode != nil && idNode.String == "proc-1" {
			procIdNode = val
			break
		}
	}

	if procIdNode == nil {
		t.Fatalf("could not find proc-1 in reconstructed state (array length: %d)", len(readPatch.Values))
	}

	// Verify the process fields
	pidNode := ir.Get(procIdNode, "pid")
	if pidNode == nil || pidNode.Int64 == nil || *pidNode.Int64 != 1234 {
		t.Errorf("expected pid 1234, got %v", pidNode)
	}

	nameNode := ir.Get(procIdNode, "name")
	if nameNode == nil || nameNode.String != "nginx" {
		t.Errorf("expected name nginx, got %v", nameNode)
	}

	stateNode := ir.Get(procIdNode, "state")
	if stateNode == nil || stateNode.String != "running" {
		t.Errorf("expected state running, got %v", stateNode)
	}
}

func TestRoundTrip_MultipleWritesThenRead(t *testing.T) {
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

	// Write multiple diffs
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

	// Read back all processes
	readRequestBody := `path: /proc/processes
match: null
`

	readReq := httptest.NewRequest("MATCH", "/api/data", bytes.NewBufferString(readRequestBody))
	readReq.Header.Set("Content-Type", "application/x-tony")
	readResp := httptest.NewRecorder()

	readBody, err := api.ParseRequestBody(readReq)
	if err != nil {
		t.Fatalf("failed to parse read request body: %v", err)
	}

	server.handleMatchData(readResp, readReq, readBody)

	if readResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for read, got %d: %s", readResp.Code, readResp.Body.String())
	}

	// Parse read response
	readDoc, err := parse.Parse(readResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse read response: %v", err)
	}

	readPatch := ir.Get(readDoc, "patch")
	if readPatch == nil {
		t.Fatal("expected patch field in read response")
	}

	// Since we used !key(id) in the diffs, the result should always be an array
	if readPatch.Type != ir.ArrayType {
		t.Fatalf("expected array type (from !key(id) diff), got %v (tag: %q)", readPatch.Type, readPatch.Tag)
	}

	// Verify all processes are present in the array
	found := make(map[string]bool)
	for _, proc := range processes {
		found[proc.id] = false
	}

	for _, val := range readPatch.Values {
		idNode := ir.Get(val, "id")
		if idNode != nil {
			found[idNode.String] = true
		}
	}

	for id, wasFound := range found {
		if !wasFound {
			t.Errorf("process %s not found in reconstructed state", id)
		}
	}
}
