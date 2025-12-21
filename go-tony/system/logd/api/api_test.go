package api

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestParseRequestBody(t *testing.T) {
	// Create a simple test request body using the new Body structure
	// Body has path: and data: fields
	requestBody := `path: /proc/processes
data:
  id: "proc-1"
  pid: 1234
`

	req := httptest.NewRequest("MATCH", "/api/data", bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/x-tony")

	body, err := ParseRequestBody(req)
	if err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	// Check that fields are populated
	if body.Path == "" {
		t.Error("expected Path to be non-empty")
	} else {
		t.Logf("Path: %v", body.Path)
	}

	if body.Data == nil {
		t.Error("expected Data to be non-nil")
	} else if body.Data.Type != ir.ObjectType {
		t.Errorf("expected Data to be object, got %v", body.Data.Type)
	} else {
		t.Logf("Data type: %v", body.Data.Type)
	}
}
