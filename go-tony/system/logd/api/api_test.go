package api

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
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

func TestSessionRequest_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "match request sync",
			input: `match:
  body:
    path: users.alice
`,
		},
		{
			name: "match request async",
			input: `id: "req-1"
match:
  body:
    path: users.alice
`,
		},
		{
			name: "patch request",
			input: `id: "req-2"
patch:
  patch:
    path: users.bob
    data:
      name: "Bob"
`,
		},
		{
			name: "subscribe request with fullState",
			input: `id: "sub-1"
watch:
  path: users
  fromCommit: 42
  fullState: true
`,
		},
		{
			name: "subscribe request without fullState",
			input: `watch:
  path: posts
  fromCommit: 0
  fullState: false
`,
		},
		{
			name: "unsubscribe request",
			input: `unwatch:
  path: users
`,
		},
		{
			name: "hello request",
			input: `hello:
  clientId: "client-123"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse input
			var req SessionRequest
			if err := req.FromTony([]byte(tt.input)); err != nil {
				t.Fatalf("FromTony failed: %v", err)
			}

			// Serialize back
			output, err := req.ToTony()
			if err != nil {
				t.Fatalf("ToTony failed: %v", err)
			}

			// Parse again to verify round-trip
			var req2 SessionRequest
			if err := req2.FromTony(output); err != nil {
				t.Fatalf("FromTony (round-trip) failed: %v", err)
			}

			// Verify key fields match
			if (req.ID == nil) != (req2.ID == nil) {
				t.Error("ID mismatch")
			}
			if req.ID != nil && req2.ID != nil && *req.ID != *req2.ID {
				t.Errorf("ID value mismatch: %q vs %q", *req.ID, *req2.ID)
			}

			t.Logf("Round-trip successful:\n%s", output)
		})
	}
}

func TestSessionResponse_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		response *SessionResponse
	}{
		{
			name: "match result",
			response: NewMatchResponse(
				stringPtr("req-1"),
				42,
				ir.FromString("Alice"),
			),
		},
		{
			name: "patch result",
			response: NewPatchResponse(
				stringPtr("req-2"),
				43,
			),
		},
		{
			name: "subscribe result with replay",
			response: NewWatchResponse(
				stringPtr("sub-1"),
				"users",
				int64Ptr(100),
			),
		},
		{
			name: "unsubscribe result",
			response: NewUnwatchResponse(
				nil,
				"users",
			),
		},
		{
			name: "state event",
			response: NewStateEvent(
				42,
				"users",
				mustParse(`{alice: {name: "Alice"}, bob: {name: "Bob"}}`),
			),
		},
		{
			name: "patch event",
			response: NewPatchEvent(
				43,
				"users.charlie",
				mustParse(`{name: "Charlie"}`),
			),
		},
		{
			name: "replay complete event",
			response: NewReplayCompleteEvent(),
		},
		{
			name: "error response",
			response: NewErrorResponse(
				stringPtr("req-5"),
				ErrCodeInvalidPath,
				"path not found",
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize
			output, err := tt.response.ToTony()
			if err != nil {
				t.Fatalf("ToTony failed: %v", err)
			}

			// Parse back
			var resp SessionResponse
			if err := resp.FromTony(output); err != nil {
				t.Fatalf("FromTony failed: %v", err)
			}

			// Verify ID matches
			if (tt.response.ID == nil) != (resp.ID == nil) {
				t.Error("ID mismatch")
			}
			if tt.response.ID != nil && resp.ID != nil && *tt.response.ID != *resp.ID {
				t.Errorf("ID value mismatch: %q vs %q", *tt.response.ID, *resp.ID)
			}

			t.Logf("Round-trip successful:\n%s", output)
		})
	}
}

func TestSessionError(t *testing.T) {
	err := NewSessionError(ErrCodeInvalidPath, "path not found")

	if err.Error() != "invalid_path: path not found" {
		t.Errorf("unexpected error string: %s", err.Error())
	}

	// Test nil error
	var nilErr *SessionError
	if nilErr.Error() != "" {
		t.Errorf("nil error should return empty string")
	}
}

func stringPtr(s string) *string {
	return &s
}

func int64Ptr(i int64) *int64 {
	return &i
}

func mustParse(s string) *ir.Node {
	node, err := parse.Parse([]byte(s))
	if err != nil {
		panic(err)
	}
	return node
}
