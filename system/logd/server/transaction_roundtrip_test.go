package server

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/parse"
	"github.com/tony-format/tony/system/logd/api"
	"github.com/tony-format/tony/system/logd/storage"
)

func TestTransactionRoundTrip_CreateThenMatch(t *testing.T) {
	// Set up temporary storage
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	//defer os.RemoveAll(tmpDir)
	t.Logf("data file %s", tmpDir)

	s, err := storage.Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	server := New(s)

	// Step 1: Create a transaction
	createRequestBody := `path: /api/transactions
match: null
patch: !key(id)
- !insert
  participantCount: 2
`

	createReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(createRequestBody))
	createReq.Header.Set("Content-Type", "application/x-tony")
	createResp := httptest.NewRecorder()

	createBody, err := api.ParseRequestBody(createReq)
	if err != nil {
		t.Fatalf("failed to parse create request body: %v", err)
	}

	server.handlePatchTransaction(createResp, createReq, createBody)

	if createResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for create, got %d: %s", createResp.Code, createResp.Body.String())
	}

	// Parse create response to get transactionId
	createDoc, err := parse.Parse(createResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}

	patchNode := ir.Get(createDoc, "patch")
	if patchNode == nil || patchNode.Type != ir.ArrayType || len(patchNode.Values) == 0 {
		t.Fatal("expected patch with array containing transaction")
	}

	transactionNode := patchNode.Values[0]
	transactionIDNode := ir.Get(transactionNode, "transactionId")
	if transactionIDNode == nil || transactionIDNode.String == "" {
		t.Fatal("expected transactionId in create response")
	}
	transactionID := transactionIDNode.String

	// Verify transactionId format (tx-{seq}-{participantCount})
	if len(transactionID) < 5 || transactionID[:3] != "tx-" {
		t.Errorf("expected transactionId to start with 'tx-', got %q", transactionID)
	}

	// Step 2: Read transaction status using MATCH
	matchRequestBody := fmt.Sprintf(`path: /api/transactions
match:
  transactionId: %q
`, transactionID)

	matchReq := httptest.NewRequest("MATCH", "/api/data", bytes.NewBufferString(matchRequestBody))
	matchReq.Header.Set("Content-Type", "application/x-tony")
	matchResp := httptest.NewRecorder()

	matchBody, err := api.ParseRequestBody(matchReq)
	if err != nil {
		t.Fatalf("failed to parse match request body: %v", err)
	}

	server.handleMatchTransaction(matchResp, matchReq, matchBody)

	if matchResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for match, got %d: %s", matchResp.Code, matchResp.Body.String())
	}

	// Parse match response
	matchDoc, err := parse.Parse(matchResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse match response: %v", err)
	}

	// Verify response structure
	if matchDoc.Type != ir.ObjectType {
		t.Fatalf("expected object response, got %v", matchDoc.Type)
	}

	// Check path
	pathNode := ir.Get(matchDoc, "path")
	if pathNode == nil || pathNode.String != "/api/transactions" {
		t.Errorf("expected path /api/transactions, got %v", pathNode)
	}

	// Check match contains transactionId
	matchNode := ir.Get(matchDoc, "match")
	if matchNode == nil {
		t.Fatal("expected match field in response")
	}
	matchTxID := ir.Get(matchNode, "transactionId")
	if matchTxID == nil || matchTxID.String != transactionID {
		t.Errorf("expected transactionId %q in match, got %v", transactionID, matchTxID)
	}

	// Check patch contains transaction data
	patchNode = ir.Get(matchDoc, "patch")
	if patchNode == nil || patchNode.Type != ir.ObjectType {
		t.Fatal("expected patch field with transaction data")
	}

	// Verify transaction fields
	statusNode := ir.Get(patchNode, "status")
	if statusNode == nil || statusNode.String != "pending" {
		t.Errorf("expected status 'pending', got %v", statusNode)
	}

	participantCountNode := ir.Get(patchNode, "participantCount")
	if participantCountNode == nil || participantCountNode.Int64 == nil || *participantCountNode.Int64 != 2 {
		t.Errorf("expected participantCount 2, got %v", participantCountNode)
	}

	participantsReceivedNode := ir.Get(patchNode, "participantsReceived")
	if participantsReceivedNode == nil || participantsReceivedNode.Int64 == nil || *participantsReceivedNode.Int64 != 0 {
		t.Errorf("expected participantsReceived 0, got %v", participantsReceivedNode)
	}

	createdAtNode := ir.Get(patchNode, "createdAt")
	if createdAtNode == nil || createdAtNode.String == "" {
		t.Error("expected createdAt field")
	}
}

func TestTransactionRoundTrip_CreateThenAbort(t *testing.T) {
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

	// Step 1: Create a transaction
	createRequestBody := `path: /api/transactions
match: null
patch: !key(id)
- !insert
  participantCount: 3
`

	createReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(createRequestBody))
	createReq.Header.Set("Content-Type", "application/x-tony")
	createResp := httptest.NewRecorder()

	createBody, err := api.ParseRequestBody(createReq)
	if err != nil {
		t.Fatalf("failed to parse create request body: %v", err)
	}

	server.handlePatchTransaction(createResp, createReq, createBody)

	if createResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for create, got %d: %s", createResp.Code, createResp.Body.String())
	}

	// Extract transactionId
	createDoc, err := parse.Parse(createResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}

	patchNode := ir.Get(createDoc, "patch")
	transactionNode := patchNode.Values[0]
	transactionIDNode := ir.Get(transactionNode, "transactionId")
	transactionID := transactionIDNode.String

	// Step 2: Abort the transaction
	abortRequestBody := fmt.Sprintf(`path: /api/transactions
match:
  transactionId: %q
patch: !delete null
`, transactionID)

	abortReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(abortRequestBody))
	abortReq.Header.Set("Content-Type", "application/x-tony")
	abortResp := httptest.NewRecorder()

	abortBody, err := api.ParseRequestBody(abortReq)
	if err != nil {
		t.Fatalf("failed to parse abort request body: %v", err)
	}

	server.handlePatchTransaction(abortResp, abortReq, abortBody)

	if abortResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for abort, got %d: %s", abortResp.Code, abortResp.Body.String())
	}

	// Parse abort response
	abortDoc, err := parse.Parse(abortResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse abort response: %v", err)
	}

	// Verify abort response structure
	patchNode = ir.Get(abortDoc, "patch")
	if patchNode == nil || patchNode.Type != ir.ArrayType || len(patchNode.Values) == 0 {
		t.Fatal("expected patch with array containing transaction")
	}

	transactionNode = patchNode.Values[0]
	statusNode := ir.Get(transactionNode, "status")
	if statusNode == nil {
		t.Fatal("expected status in abort response")
	}

	// Check if status is a replace operation
	if statusNode.Tag != "!replace" {
		t.Errorf("expected status to be !replace operation, got tag %q", statusNode.Tag)
	}

	// Verify status changed to aborted
	toNode := ir.Get(statusNode, "to")
	if toNode == nil || toNode.String != "aborted" {
		t.Errorf("expected status.to 'aborted', got %v", toNode)
	}

	participantsDiscardedNode := ir.Get(transactionNode, "participantsDiscarded")
	if participantsDiscardedNode == nil || participantsDiscardedNode.Int64 == nil || *participantsDiscardedNode.Int64 != 0 {
		t.Errorf("expected participantsDiscarded 0, got %v", participantsDiscardedNode)
	}

	// Step 3: Verify transaction is aborted by reading it
	matchRequestBody := fmt.Sprintf(`path: /api/transactions
match:
  transactionId: %q
`, transactionID)

	matchReq := httptest.NewRequest("MATCH", "/api/data", bytes.NewBufferString(matchRequestBody))
	matchReq.Header.Set("Content-Type", "application/x-tony")
	matchResp := httptest.NewRecorder()

	matchBody, err := api.ParseRequestBody(matchReq)
	if err != nil {
		t.Fatalf("failed to parse match request body: %v", err)
	}

	server.handleMatchTransaction(matchResp, matchReq, matchBody)

	if matchResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for match, got %d: %s", matchResp.Code, matchResp.Body.String())
	}

	matchDoc, err := parse.Parse(matchResp.Body.Bytes())
	if err != nil {
		t.Fatalf("failed to parse match response: %v", err)
	}

	patchNode = ir.Get(matchDoc, "patch")
	statusNode = ir.Get(patchNode, "status")
	if statusNode == nil || statusNode.String != "aborted" {
		t.Errorf("expected status 'aborted', got %v", statusNode)
	}
}

func TestTransactionRoundTrip_AbortNonExistent(t *testing.T) {
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

	// Try to abort non-existent transaction
	abortRequestBody := `path: /api/transactions
match:
  transactionId: "tx-99999-2"
patch: !delete null
`

	abortReq := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(abortRequestBody))
	abortReq.Header.Set("Content-Type", "application/x-tony")
	abortResp := httptest.NewRecorder()

	abortBody, err := api.ParseRequestBody(abortReq)
	if err != nil {
		t.Fatalf("failed to parse abort request body: %v", err)
	}

	server.handlePatchTransaction(abortResp, abortReq, abortBody)

	if abortResp.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for non-existent transaction, got %d: %s", abortResp.Code, abortResp.Body.String())
	}
}

func TestTransactionRoundTrip_MatchNonExistent(t *testing.T) {
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

	// Try to match non-existent transaction
	matchRequestBody := `path: /api/transactions
match:
  transactionId: "tx-99999-2"
`

	matchReq := httptest.NewRequest("MATCH", "/api/data", bytes.NewBufferString(matchRequestBody))
	matchReq.Header.Set("Content-Type", "application/x-tony")
	matchResp := httptest.NewRecorder()

	matchBody, err := api.ParseRequestBody(matchReq)
	if err != nil {
		t.Fatalf("failed to parse match request body: %v", err)
	}

	server.handleMatchTransaction(matchResp, matchReq, matchBody)

	if matchResp.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for non-existent transaction, got %d: %s", matchResp.Code, matchResp.Body.String())
	}
}
