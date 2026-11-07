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
	sortKeys   bool
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

// ParseComments(true) keeps comments, as the IR holds them: a head comment as a
// CommentType node wrapping the value, a line comment in the value's Comment
// field. By default they are dropped.
func ParseComments(v bool) ParseOption {
	return func(o *parseOpts) { o.comments = v }
}

// ParsePositions records in m where each value and key node was read from.
func ParsePositions(m map[*ir.Node]*token.Pos) ParseOption {
	return func(o *parseOpts) {
		o.positions = m
	}
}

// NoBrackets leaves an object or array written in brackets untagged. By default
// one carries [ir.BracketTag], so that it is encoded in brackets again.
func NoBrackets() ParseOption {
	return func(o *parseOpts) { o.noBrackets = true }
}

// SortKeys answers every string-keyed object with its fields sorted by key, in
// byte order -- the order [ir.FromMap] gives. By default fields keep the order
// they were written in.
//
// An object holding a merge key keeps its written order: a merge key is not a
// name, and where it sits is part of what it means. An int-keyed object
// ([ir.IntKeysTag]) is sorted either way.
//
// The fields are sorted after comments are associated, so a comment moves with
// the value it belongs to. A comment above an object's first key belongs to the
// object, which begins there too, so it stays at the top of the object, above
// whichever key sorts first. The key and value nodes are moved rather than
// rebuilt, so the positions [ParsePositions] records still point into the tree.
func SortKeys() ParseOption {
	return func(o *parseOpts) { o.sortKeys = true }
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
