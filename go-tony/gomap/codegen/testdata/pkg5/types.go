package pkg5

import "github.com/signadot/tony-format/go-tony/format"

// ExternalTextUnmarshaler tests that external package types implementing
// TextUnmarshaler are correctly detected during type resolution.
//
//tony:schemagen=externalunmarshaler
type ExternalTextUnmarshaler struct {
	Format *format.Format `tony:"field=format"`
}
