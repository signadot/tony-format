package libdiff

import (
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"

	diffpatch "github.com/sergi/go-diff/diffmatchpatch"
)

// we use int keyed map and
//
//  1. record the type of each node, for non-string scalar types...
//     we use the summary value <type>-<value> where <value> is the string
//     representation
//  2. diff the sequence of summaries
//  3. For every matching type in the result, if that type is not
//     scalar, we recurse
//  4. For every non-matching type, we add an int-keyed
//     map item with the corresponding diff operation tagged
func DiffArrayByIndex(from, to *ir.Node, df DiffFunc) *ir.Node {
	m := map[string]rune{}
	fromRunes := mapValues(m, from)
	toRunes := mapValues(m, to)
	diffCfg := diffpatch.New()
	diffs := diffCfg.DiffMainRunes(fromRunes, toRunes, false)
	resMap := make(map[uint32]*ir.Node, len(diffs))

	// ri is a position in the sequence the two arrays share.  Every slot
	// advances it by one and stands for one element of each array which has
	// one: a delete has only a from, an insert only a to, an equal and a
	// replace both.  The applier walks the same positions and reads a missing
	// one as an unchanged element, so a slot which does not account for
	// exactly this throws off every key after it.
	fi, ti, ri := 0, 0, uint32(0)
	var (
		delIndex *uint32
		delNode  *ir.Node
	)
	for i := range diffs {
		diff := &diffs[i]
		switch diff.Type {
		case diffpatch.DiffDelete:
			for _, r := range diff.Text {
				_ = r
				resMap[ri] = MakeDiff(from.Values[fi], nil)
				tmp := ri
				delIndex = &tmp
				delNode = from.Values[fi]
				ri++
				fi++
			}
		case diffpatch.DiffEqual:
			delIndex, delNode = nil, nil
			for _, r := range diff.Text {
				_ = r
				di := df(from.Values[fi], to.Values[ti])
				if di != nil {
					resMap[ri] = di
				}
				ri++
				fi++
				ti++
			}
		case diffpatch.DiffInsert:
			for _, r := range diff.Text {
				_ = r
				if delIndex != nil && *delIndex == ri-1 {
					// this insert lands on the element the slot before it
					// deleted, which is a replace: one slot for one element of
					// each array, so ri does not advance again.  delNode is
					// the element itself, tags and all -- the !delete written
					// into resMap holds its tag as an argument instead, and a
					// !replace states its from: whole.
					resMap[*delIndex] = MakeDiff(delNode, to.Values[ti])
				} else {
					resMap[ri] = MakeDiff(nil, to.Values[ti])
					ri++
				}
				ti++
				delIndex, delNode = nil, nil
			}
			delIndex, delNode = nil, nil
		}
	}
	// an arraydiff describes the elements of an array, never its tag, so a
	// change to that is composed after it, as DiffObject and DiffString do
	if len(resMap) == 0 {
		if from.Tag == to.Tag {
			return nil
		}
		return ir.Null().WithTag(mkTagDiff(from.Tag, to.Tag))
	}
	tag := ArrayDiffTag
	if from.Tag != to.Tag {
		tag = ir.TagCompose(ArrayDiffTag, nil, mkTagDiff(from.Tag, to.Tag))
	}
	return ir.FromIntKeysMap(resMap).WithTag(tag)
}

func mapValues(m map[string]rune, node *ir.Node) []rune {
	rs := make([]rune, len(node.Values))
	for i, v := range node.Values {
		sum := summaryStr(v)
		r, ok := m[sum]
		if !ok {
			r = rune(len(m))
			m[sum] = r
		}
		rs[i] = r
	}
	return rs
}

// summaryStr answers what an element IS, for the alignment above: two elements
// with the same summary are candidates for the same slot, and what actually
// changed between a pair is left to df.
//
// A comment describes an element and is not what the element IS, so it is seen
// through here, as every walk that asks what kind of node it is standing on does
// (ir.Uncomment). Two elements differing only in a comment therefore align as one
// element, and the comment change is the delta df reports about the pair --
// which is the whole point of asking for comments. Before this a commented
// element reached the default below and panicked, so `o diff -c` died on any
// array holding one, whether or not the comment was what changed.
func summaryStr(node *ir.Node) string {
	node = ir.Uncomment(node)
	switch node.Type {
	case ir.ObjectType, ir.ArrayType, ir.NullType:
		return node.Type.String()
	case ir.BoolType:
		return node.Type.String() + "-" + strconv.FormatBool(node.Bool)
	case ir.StringType:
		if strings.Contains(node.String, "\n") {
			return node.Type.String() + "/m"
		}
		return node.Type.String() + "-" + node.String
	case ir.NumberType:
		if node.Int64 != nil {
			return node.Type.String() + "-i-" + strconv.FormatInt(*node.Int64, 10)
		}
		if node.Float64 != nil {
			return node.Type.String() + "-f-" + strconv.FormatFloat(*node.Float64, 'f', -1, 64)
		}
		panic("number")
	default:
		panic("type")
	}
}
