package api

import (
	"fmt"
	"io"
	"net/http"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Body represents the common structure for all requests using the path: match: patch: meta: layout.
//
//tony:schemagen=body
type Body struct {
	Path  string              `tony:"field=path"`
	Match *ir.Node            `tony:"field=match"`
	Patch *ir.Node            `tony:"field=patch"`
	Meta  map[string]*ir.Node `tony:"field=meta"`
}

//tony:schemagen=encoding-options
type EncodingOptions struct {
	Wire     bool `tony:"field=wire"`
	Brackets bool `tony:"field=brackets"`
}

//tony:schemagen=match-meta
type MatchMeta struct {
	EncodingOptions
	SeqID *int64 `tony:"field=seq"`
}

//tony:schemagen=match
type Match struct {
	Meta MatchMeta `tony:"field=meta"`
	Body Body      `tony:"field=body"`
}

//tony:schemagen=patch-meta
type PatchMeta struct {
	EncodingOptions
	Tx          *string `tony:"field=tx"`
	MaxDuration string  `tony:"field=maxDuration"`

	// output fields
	Seq  *int64 `tony:"field=seq"`
	When string `tony:"field=when"`
}

//tony:schemagen=patch
type Patch struct {
	Meta PatchMeta `tony:"field=meta"`
	Body Body      `tony:"field=body"`
}

//tony:schemagen=watch-meta
type WatchMeta struct {
	EncodingOptions
	From *int64 `tony:"field=from"`
	To   *int64 `tony:"field=to"`
}

// ParseRequestBody parses the request body as a Tony document and extracts the RequestBody structure.
func ParseRequestBody(r *http.Request) (*Body, error) {
	// Check Content-Type
	if r.Header.Get("Content-Type") != "application/x-tony" {
		return nil, fmt.Errorf("Content-Type must be application/x-tony")
	}

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	// Use generated FromTony method to populate RequestBody
	reqBody := &Body{}
	if err := reqBody.FromTony(bodyBytes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal request body: %w", err)
	}

	return reqBody, nil
}
