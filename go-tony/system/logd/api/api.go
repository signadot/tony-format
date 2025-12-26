package api

import (
	"time"

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
//tony:schemagen=patch,notag
type Patch struct {
	Match *PathData `tony:"field=match"`
	// Patch PathData  `tony:"field=patch"`
	PathData
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
