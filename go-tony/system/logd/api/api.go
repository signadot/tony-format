package api

import (
	"github.com/signadot/tony-format/go-tony/ir"
)

// PathData represents a path and optional data for patches and matches.
//
//tony:schemagen=path-data,notag
type PathData struct {
	Path string   `tony:"field=path"`
	Data *ir.Node `tony:"field=data"`
}

// Patch represents a patch operation with optional match precondition.
// Used by the transaction layer for atomic multi-participant operations.
//
// Author is who the participant's write is by, resolved already (the request's, or
// the session's): it is what the transaction's record keeps for the participant, and
// what the commit is recorded under. Empty is none.
//
//tony:schemagen=patch,notag
type Patch struct {
	Match  *PathData `tony:"field=match"`
	Author string    `tony:"field=author,omitzero"`
	// Patch PathData  `tony:"field=patch"`
	PathData
}
