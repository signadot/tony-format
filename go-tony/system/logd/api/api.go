package api

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Body represents the common structure for all requests using the path: match: patch: meta: layout.
//
//tony:schemagen=body
type Body struct {
	Path string   `tony:"field=path"`
	Data *ir.Node `tony:"field=data"`
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
	Tx          *string  `tony:"field=tx"`
	MaxDuration Duration `tony:"field=maxDuration"`
	Seq         *int64   `tony:"field=seq"` // Seq when supplied asserts that seq is the latest value for patched data, on return, if successful, seq shows the commit resulting from applying the changes.

	When *time.Time `tony:"field=when"`
}

//tony:schemagen=patch
type Patch struct {
	Meta  PatchMeta `tony:"field=meta"`
	Match *Body     `tony:"field=match"`
	Patch Body      `tony:"field=patch"`
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

type Duration time.Duration

func (dur Duration) MarshalText() ([]byte, error) {
	ds := time.Duration(dur).String()
	return []byte(ds), nil
}

func (dur *Duration) UnmarshalText(d []byte) error {
	p, err := time.ParseDuration(string(d))
	if err != nil {
		return err
	}
	*dur = Duration(p)
	return nil
}
