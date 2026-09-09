package index

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// Cover is what a STATEMENT -- a node an entry states something at -- says about
// everything at and beneath its path, and so what it does to the statements before it
// there (scope_plan.md; the rules were probed against the fold before they were written,
// rebuild_plan.md phase 7):
//
//	total   the result at the path is the operand, or absence, whatever was there:
//	        !insert, which applies its child against absence, and !delete. A claim is
//	        !insert.raw.
//	whole   the value at the path is replaced and everything beneath it with it, but at
//	        the path ITSELF the merge may keep a comment or a tag the earlier statement
//	        carried: a plain scalar, or an array of plain scalars.
//	none    anything else. An object with fields is not a statement, its fields are; an
//	        empty object merges nothing away; an array with object elements merges element
//	        by element; an operation other than the two above says something relative to
//	        its neighbours; a bare !raw merges its subtree as data and covers what a value
//	        of that shape covers.
//
// A statement NEEDS whole when it is an untagged, uncommented scalar or array of them, an
// empty object, or a total cover itself; otherwise it needs total. It is DOMINATED by a
// total cover at or above its path, by a whole cover strictly above it, or by a whole cover
// at its own path when it needs only whole -- always by a LATER statement of the same
// scope. A dominated statement contributes nothing to the fold from the dominating one on,
// which is what lets it be skipped by a read (the footprint) and dropped by compaction.
type Cover uint8

const (
	CoverNone Cover = iota
	CoverWhole
	CoverTotal
)

func (c Cover) String() string {
	switch c {
	case CoverWhole:
		return "whole"
	case CoverTotal:
		return "total"
	}
	return "none"
}

// dominates says whether a LATER statement offering c retires an earlier one that needs
// `needs`, at the same path when same is true and strictly above it otherwise.
func (c Cover) dominates(needs Cover, same bool) bool {
	switch c {
	case CoverTotal:
		return true
	case CoverWhole:
		return !same || needs == CoverWhole
	}
	return false
}

// Classify answers a statement's cover: what a node an entry states something at offers
// to the statements before it and needs from the ones after it. data says every tag on
// and beneath the node is data -- the node sits inside a merging !raw -- so nothing is an
// operation.
func Classify(root *ir.Node, data bool) (offers, needs Cover) {
	commented := root != nil && root.Type == ir.CommentType
	n := ir.Uncomment(root)
	if n == nil {
		return CoverNone, CoverTotal
	}
	if !data {
		// The operation a node is applied by is the first one in its chain: !insert.raw is
		// an insert, and a label ahead of the operation is the value's.
		switch firstOperator(n.Tag) {
		case "insert":
			// The result is the operand whatever was there (insertOp.Patch applies it
			// against absence); a comment on the wrapper may land with it.
			if commented {
				return CoverTotal, CoverTotal
			}
			return CoverTotal, CoverWhole
		case "delete":
			return CoverTotal, CoverWhole
		case "raw":
			// The escape merges, so the statement is the value it wraps, every tag beneath
			// it a data tag. A data tag AHEAD of the escape rides on the value too.
			pre, _, _, child, err := mergeop.SplitChild(n)
			if err != nil || child == nil {
				return CoverNone, CoverTotal
			}
			offers, needs = statementData(child, commented)
			if ir.StripPresentation(pre) != "" && needs < CoverTotal {
				needs = CoverTotal
			}
			return offers, needs
		case "":
		default:
			return CoverNone, CoverTotal
		}
	}
	return statementData(n, commented)
}

// statementData classifies a node whose tags are data: what a value of its shape offers
// to the statements before it and needs from the ones after, with nothing to dispatch.
// Presentation is not a tag here: how a value was written says nothing a merge keeps.
func statementData(n *ir.Node, commented bool) (offers, needs Cover) {
	switch n.Type {
	case ir.ObjectType:
		if commented {
			return CoverNone, CoverTotal
		}
		return CoverNone, CoverWhole
	case ir.ArrayType:
		if !plainValue(n) {
			return CoverNone, CoverTotal
		}
	default:
		if ir.StripPresentation(n.Tag) != "" || commented {
			return CoverWhole, CoverTotal
		}
	}
	if commented {
		return CoverWhole, CoverTotal
	}
	return CoverWhole, CoverWhole
}

// firstOperator is the merge operation a tag chain names first, without its '!', or ""
// when it names none.
func firstOperator(tag string) string {
	for t := tag; t != ""; {
		head, _, rest := ir.TagArgs(t)
		if head == "" {
			return ""
		}
		if mergeop.Lookup(head[1:]) != nil {
			return head[1:]
		}
		if rest == t {
			return ""
		}
		t = rest
	}
	return ""
}

// plainValue is a value with nothing on it that a merge might keep: no tag, no comment,
// and, in an array, no element that is not itself one -- an object element merges rather
// than replaces.
func plainValue(n *ir.Node) bool {
	if n == nil || ir.StripPresentation(n.Tag) != "" {
		return false
	}
	switch n.Type {
	case ir.CommentType, ir.ObjectType:
		return false
	case ir.ArrayType:
		for _, v := range n.Values {
			if !plainValue(v) {
				return false
			}
		}
	}
	return true
}
