package libdiff

import (
	"strconv"
	"strings"

	"github.com/tony-format/tony/ir"

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

	fi, ti, ri := 0, 0, uint32(0)
	var delIndex *uint32
	for i := range diffs {
		diff := &diffs[i]
		switch diff.Type {
		case diffpatch.DiffDelete:
			for _, r := range diff.Text {
				_ = r
				resMap[ri] = MakeDiff(from.Values[fi], nil)
				tmp := ri
				delIndex = &tmp
				ri++
				fi++
			}
		case diffpatch.DiffEqual:
			delIndex = nil
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
					resMap[ri-1] = MakeDiff(resMap[ri-1].WithTag(""), to.Values[ti])
				} else {
					resMap[ri] = MakeDiff(nil, to.Values[ti])
				}
				ri++
				ti++
				delIndex = nil
			}
			delIndex = nil
		}
	}
	if len(resMap) == 0 {
		return nil
	}
	return ir.FromIntKeysMap(resMap).WithTag(ArrayDiffTag)
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

func summaryStr(node *ir.Node) string {
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
