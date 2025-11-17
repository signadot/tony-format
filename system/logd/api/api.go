package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/parse"
)

// RequestBody represents the common structure for all requests using the path: match: patch: meta: layout.
type RequestBody struct {
	Path  *ir.Node `tony:"name=path"`
	Match *ir.Node `tony:"name=match"`
	Patch *ir.Node `tony:"name=patch"`
	Meta  *ir.Node `tony:"name=meta"`
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

	// Extract path, match, patch, meta fields
	reqBody := &RequestBody{}
	if doc.Type == ir.ObjectType {
		for i, field := range doc.Fields {
			value := doc.Values[i]
			switch field.String {
			case "path":
				reqBody.Path = value
			case "match":
				reqBody.Match = value
			case "patch":
				reqBody.Patch = value
			case "meta":
				reqBody.Meta = value
			}
		}
	}

	return reqBody, nil
}
