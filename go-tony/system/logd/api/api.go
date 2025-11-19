package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// RequestBody represents the common structure for all requests using the path: match: patch: meta: layout.
//
//tony:schema=request_body
type RequestBody struct {
	Path  string              `tony:"field=path"`
	Match *ir.Node            `tony:"field=match"`
	Patch *ir.Node            `tony:"field=patch"`
	Meta  map[string]*ir.Node `tony:"field=meta"`
}

// ParseRequestBody parses the request body as a Tony document and extracts the RequestBody structure.
func ParseRequestBody(r *http.Request) (*RequestBody, error) {
	// Check Content-Type
	if r.Header.Get("Content-Type") != "application/x-tony" {
		return nil, fmt.Errorf("Content-Type must be application/x-tony")
	}

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	// Parse as Tony document
	doc, err := parse.Parse(bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Tony document: %w", err)
	}

	// Use generated FromTonyIR method to populate RequestBody
	reqBody := &RequestBody{}
	if err := reqBody.FromTonyIR(doc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request body: %w", err)
	}

	return reqBody, nil
}
