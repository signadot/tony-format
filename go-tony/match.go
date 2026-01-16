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

// Match matches doc against a pattern. This is the backwards-compatible
// version that doesn't use context. Use MatchWith for schema-aware matching.
func Match(doc, match *ir.Node) (bool, error) {
	return MatchWith(doc, match, nil)
}

// MatchWith matches doc against a pattern with the given context.
// The context carries schema definitions for .[ref] expansion and behavioral options.
func MatchWith(doc, match *ir.Node, ctx *mergeop.OpContext) (bool, error) {
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
		// Create a MatchFunc that threads ctx through recursive calls
		matchFunc := func(d, p *ir.Node, c *mergeop.OpContext) (bool, error) {
			return MatchWith(d, p, c)
		}
		return opInst.Match(doc, ctx, matchFunc)
	}
	if doc.Type != match.Type && match.Type != ir.NullType {
		return false, nil
	}
	switch match.Type {
	case ir.ObjectType:
		return tagMatchObjWith(doc, match, tag, ctx)
	case ir.ArrayType:
		return tagMatchArrayWith(doc, match, tag, ctx)
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

func tagMatchObjWith(doc, match *ir.Node, tag string, ctx *mergeop.OpContext) (bool, error) {
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
		subMatch, err := MatchWith(doc.Values[i], my, ctx)
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

func tagMatchArrayWith(doc, match *ir.Node, tag string, ctx *mergeop.OpContext) (bool, error) {
	if len(doc.Values) != len(match.Values) {
		return false, nil
	}
	for i := range doc.Values {
		subMatch, err := MatchWith(doc.Values[i], match.Values[i], ctx)
		if err != nil {
			return false, err
		}
		if !subMatch {
			return false, nil
		}
	}
	return true, nil
}

// Keep old functions for backwards compatibility within this package
func tagMatchObj(doc, match *ir.Node, tag string) (bool, error) {
	return tagMatchObjWith(doc, match, tag, nil)
}

func tagMatchArray(doc, match *ir.Node, tag string) (bool, error) {
	return tagMatchArrayWith(doc, match, tag, nil)
}

// Trim filters a document to only include fields/values that are present in the match criteria.
// It recursively processes objects and arrays, removing fields that aren't in the match.
// The result preserves the tag from the original document.
// Returns nil if the doc doesn't match the criteria (used to signal exclusion).
func Trim(match, doc *ir.Node) *ir.Node {
	// Check for tags first - tags like !or, !not, !glob change matching semantics,
	// not structure. If the match has a tag, verify doc matches it
	// and return the doc as-is (since tags define matching criteria, not structure).
	tag, _, _, err := mergeop.SplitChild(match)
	if err == nil && tag != "" {
		// This is a tagged match (like !or, !glob, !and, !not, etc.)
		// Tags define matching semantics, not structure to preserve.
		// If doc matches the pattern, return doc as-is.
		// If doc doesn't match, return nil to signal exclusion.
		matched, _ := MatchWith(doc, match, nil)
		if matched {
			return doc.Clone()
		}
		return nil
	}

	switch match.Type {
	case ir.ObjectType:
		docMap := ir.ToMap(doc)
		matchMap := ir.ToMap(match)
		for i, field := range doc.Fields {
			matchVal := matchMap[field.String]
			if matchVal == nil {
				delete(docMap, field.String)
				continue
			}
			docVal := doc.Values[i]
			trimmed := Trim(matchVal, docVal)
			if trimmed == nil {
				delete(docMap, field.String)
			} else {
				docMap[field.String] = trimmed
			}
		}
		return ir.FromMap(docMap).WithTag(doc.Tag)
	case ir.ArrayType:
		// For arrays, we need to match each match element against doc elements
		// and keep only the matching ones, trimmed
		var res []*ir.Node
		used := make([]bool, len(doc.Values))

		for _, matchElem := range match.Values {
			// Find the first unused doc element that matches this match element
			for i, docElem := range doc.Values {
				if used[i] {
					continue
				}
				matched, err := Match(docElem, matchElem)
				if err != nil {
					// If matching fails, skip this doc element
					continue
				}
				if matched {
					// Found a match - trim it and add to result
					trimmed := Trim(matchElem, docElem)
					if trimmed != nil {
						res = append(res, trimmed)
					}
					used[i] = true
					break
				}
			}
		}
		return ir.FromSlice(res).WithTag(doc.Tag)
	default:
		return doc.Clone()
	}
}
