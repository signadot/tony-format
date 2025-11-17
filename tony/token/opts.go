package token

import (
	"github.com/signadot/tony-format/tony/format"
)

type tokenOpts struct {
	format format.Format
}
type TokenOpt func(*tokenOpts)

func TokenTony() TokenOpt {
	return func(o *tokenOpts) { o.format = format.TonyFormat }
}
func TokenYAML() TokenOpt {
	return func(o *tokenOpts) { o.format = format.YAMLFormat }
}
func TokenJSON() TokenOpt {
	return func(o *tokenOpts) { o.format = format.JSONFormat }
}
