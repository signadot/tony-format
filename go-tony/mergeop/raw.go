package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

// Raw returns the !raw symbol, the escape from operator interpretation.
//
// The patch grammar and the data grammar share one tag namespace, so without
// an escape a value whose tag happens to name a registered op is always
// interpreted rather than stored.  A tony document which itself contains tony
// operators — a match, a patch, a rule — could not be written at all.
//
// As a patch, !raw MERGES its subtree as data: the walk is the one a patch of
// the same shape takes -- an object merges field by field, an array by
// position, a scalar replaces -- with nothing beneath it interpreted, at any
// depth, and the !raw tag itself is consumed so the subtree lands with its own
// tags intact. Escaping is not replacing: a merge which reads operator tags as
// data is still a merge in which nothing beneath is interpreted, and the
// document keeps what the raw value does not mention. "The value is exactly
// this, as data" is !insert.raw, which applies the escaped value against
// absence, and is the chain a diff already emits for a value that holds an
// operation (mgg9nvt6h12krn6dksn0).
//
// As a match, !raw compares its subtree to the doc as literal data: tags are
// compared, not evaluated, and the comparison is exact rather than the partial
// object match of an ordinary pattern.  Put !raw at the depth where literal
// comparison starts; the enclosing pattern keeps ordinary match semantics.
//
// !raw executes nothing, so it is never Unsafe.
func Raw() Symbol {
	return rawSym
}

var rawSym = &rawSymbol{name: rawName}

const (
	rawName name = "raw"
)

type rawSymbol struct {
	name
}

func (s rawSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &rawOp{op: op{name: s.name, child: child}}, nil
}

type rawOp struct {
	op
}

func (r rawOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("raw op patch on %s\n", doc.Path())
	}
	// The ordinary walk with the dispatch off (OpContext.AsData). Answering a clone of
	// the child was ONE way to interpret nothing; it was also the way that threw the
	// document away.
	return pf(doc, r.child, ctx.AsData())
}

func (r rawOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("raw op match on %s\n", doc.Path())
	}
	return RawEqual(doc, r.child), nil
}

// RawEqual reports whether doc and pattern are the same data, comparing tags
// literally and interpreting no operations.  Comments and formatting-only tags
// are ignored, and object fields are compared as a set rather than in order,
// so a value compares equal to itself after any encode/parse round trip.
func RawEqual(doc, pattern *ir.Node) bool {
	doc, pattern = uncomment(doc), uncomment(pattern)
	if doc == nil || pattern == nil {
		return doc == pattern
	}
	if doc.Type != pattern.Type {
		return false
	}
	if dataTag(doc.Tag) != dataTag(pattern.Tag) {
		return false
	}
	switch doc.Type {
	case ir.ObjectType:
		if len(doc.Fields) != len(pattern.Fields) {
			return false
		}
		pMap := make(map[string]*ir.Node, len(pattern.Fields))
		for i := range pattern.Fields {
			pMap[pattern.Fields[i].String] = pattern.Values[i]
		}
		for i := range doc.Fields {
			pv, present := pMap[doc.Fields[i].String]
			if !present {
				return false
			}
			if !RawEqual(doc.Values[i], pv) {
				return false
			}
		}
		return true
	case ir.ArrayType:
		if len(doc.Values) != len(pattern.Values) {
			return false
		}
		for i := range doc.Values {
			if !RawEqual(doc.Values[i], pattern.Values[i]) {
				return false
			}
		}
		return true
	case ir.StringType:
		return doc.String == pattern.String
	case ir.BoolType:
		return doc.Bool == pattern.Bool
	case ir.NullType:
		return true
	case ir.NumberType:
		if (doc.Int64 == nil) != (pattern.Int64 == nil) {
			return false
		}
		if (doc.Float64 == nil) != (pattern.Float64 == nil) {
			return false
		}
		switch {
		case doc.Int64 != nil:
			return *doc.Int64 == *pattern.Int64
		case doc.Float64 != nil:
			return *doc.Float64 == *pattern.Float64
		}
		return doc.Number == pattern.Number
	}
	return false
}

// uncomment yields the node a comment node stands for.
func uncomment(n *ir.Node) *ir.Node {
	for n != nil && n.Type == ir.CommentType {
		if len(n.Values) == 0 {
			return nil
		}
		n = n.Values[0]
	}
	return n
}

// dataTag drops the tags which record how a node was written rather than what
// it is, so that literal comparison survives an encode/parse round trip.
func dataTag(tag string) string {
	return ir.StripPresentation(tag)
}
