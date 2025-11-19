package api

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestParseRequestBody(t *testing.T) {
	// Create a simple test request body
	requestBody := `path: /proc/processes
match: null
patch: !key(id)
- !insert
  id: "proc-1"
  pid: 1234
`

	req := httptest.NewRequest("PATCH", "/api/data", bytes.NewBufferString(requestBody))
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

	if body.Match == nil {
		t.Error("expected Match to be non-nil")
	} else {
		t.Logf("Match type: %v", body.Match.Type)
	}

	if body.Patch == nil {
		t.Error("expected Patch to be non-nil")
	} else {
		t.Logf("Patch type: %v", body.Patch.Type)
	}
}
