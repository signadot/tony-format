package parse

import (
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

type parseOpts struct {
	format     format.Format
	comments   bool
	positions  map[*ir.Node]*token.Pos
	noBrackets bool
}

func (o *parseOpts) TokenizeOpts() []token.TokenOpt {
	switch o.format {
	case format.JSONFormat:
		return []token.TokenOpt{token.TokenJSON()}
	case format.TonyFormat:
		return []token.TokenOpt{token.TokenTony()}
	case format.YAMLFormat:
		return []token.TokenOpt{token.TokenYAML()}
	}
	return nil
}

type ParseOption func(*parseOpts)

func ParseYAML() ParseOption {
	return ParseFormat(format.YAMLFormat)
}
func ParseTony() ParseOption {
	return ParseFormat(format.TonyFormat)
}
func ParseJSON() ParseOption {
	return ParseFormat(format.JSONFormat)
}
func ParseFormat(f format.Format) ParseOption {
	return func(o *parseOpts) { o.format = f }
}
func ParseComments(v bool) ParseOption {
	return func(o *parseOpts) { o.comments = v }
}
func ParsePositions(m map[*ir.Node]*token.Pos) ParseOption {
	return func(o *parseOpts) {
		o.positions = m
	}
}
func NoBrackets() ParseOption {
	return func(o *parseOpts) { o.noBrackets = true }
}

// GetPositions extracts the positions map from the provided options.
// This allows consumers (like FromTony methods) to access position information.
func GetPositions(opts ...ParseOption) map[*ir.Node]*token.Pos {
	pOpts := &parseOpts{}
	for _, f := range opts {
		f(pOpts)
	}
	return pOpts.positions
}
