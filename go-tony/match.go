package tony

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

type MatchConfig struct {
	Comments bool
	Tags     bool
}

type MatchOpt func(*MatchConfig)

func MatchComments(v bool) MatchOpt {
	return func(c *MatchConfig) { c.Comments = v }
}
func MatchTags(v bool) MatchOpt {
	return func(c *MatchConfig) { c.Tags = v }
}

func Match(doc, match *ir.Node) (bool, error) {
	if debug.Match() {
		debug.Logf("match type %s at %s with tag %q\n", match.Type, match.Path(), match.Tag)
	}
	tag, args, child, err := mergeop.SplitChild(match)
	if err != nil {
		return false, err
	}
	if tag != "" {
		op := mergeop.Lookup(tag)
		if op == nil {
			return false, fmt.Errorf("no mergeop for tag %q", tag)
		}
		opInst, err := op.Instance(child, args)
		if err != nil {
			return false, err
		}
		return opInst.Match(doc, Match)
	}
	if doc.Type != match.Type && match.Type != ir.NullType {
		return false, nil
	}
	switch match.Type {
	case ir.ObjectType:
		return tagMatchObj(doc, match, tag)
	case ir.ArrayType:
		return tagMatchArray(doc, match, tag)
	case ir.StringType:
		return doc.String == match.String, nil
	case ir.BoolType:
		return doc.Bool == match.Bool, nil
	case ir.NullType:
		return true, nil
	case ir.NumberType:
		if (doc.Int64 == nil) != (match.Int64 == nil) {
			return false, nil
		}
		if (doc.Float64 == nil) != (match.Float64 == nil) {
			return false, nil
		}
		if doc.Int64 != nil {
			return *doc.Int64 == *match.Int64, nil
		}
		if doc.Float64 != nil {
			return *doc.Float64 == *match.Float64, nil
		}
		return doc.Number == match.Number, nil
	}
	return false, nil
}

func tagMatchObj(doc, match *ir.Node, tag string) (bool, error) {
	mMap := make(map[string]*ir.Node, len(match.Fields))
	for i, field := range match.Fields {
		child := match.Values[i]
		mMap[field.String] = child
	}
	count := 0
	for i := range doc.Fields {
		field := doc.Fields[i]
		my := mMap[field.String]
		if my == nil {
			continue
		}
		subMatch, err := Match(doc.Values[i], my)
		if err != nil {
			return false, err
		}
		if !subMatch {
			return false, nil
		}
		count++
	}
	return count == len(mMap), nil
}

func tagMatchArray(doc, match *ir.Node, tag string) (bool, error) {
	if len(doc.Values) != len(match.Values) {
		return false, nil
	}
	for i := range doc.Values {
		subMatch, err := Match(doc.Values[i], match.Values[i])
		if err != nil {
			return false, err
		}
		if !subMatch {
			return false, nil
		}
	}
	return true, nil
}
