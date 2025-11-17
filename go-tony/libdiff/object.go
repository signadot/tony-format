package libdiff

import (
	"github.com/signadot/tony-format/go-tony/ir"

	diffpatch "github.com/sergi/go-diff/diffmatchpatch"
)

// 1 diff field names
// for every different field name add  node
// for every same field name, recurse on the value
func DiffObject(from, to *ir.Node, df DiffFunc) *ir.Node {
	fieldMap := map[string]rune{}
	runeMap := map[rune]string{}
	fromRunes := mapFieldsTo(fieldMap, runeMap, from)
	toRunes := mapFieldsTo(fieldMap, runeMap, to)
	diffCfg := diffpatch.New()
	diffs := diffCfg.DiffMainRunes(fromRunes, toRunes, false)
	resMap := map[string]*ir.Node{}
	fi, ti := 0, 0
	for i := range diffs {
		diff := &diffs[i]
		switch diff.Type {
		case diffpatch.DiffDelete:
			for _, r := range diff.Text {
				resMap[runeMap[r]] = MakeDiff(from.Values[fi], nil)
				fi++
			}
		case diffpatch.DiffEqual:
			for _, r := range diff.Text {
				fRes := df(from.Values[fi], to.Values[ti])
				if fRes != nil {
					resMap[runeMap[r]] = fRes
				}
				fi++
				ti++
			}
		case diffpatch.DiffInsert:
			for _, r := range diff.Text {
				resMap[runeMap[r]] = MakeDiff(nil, to.Values[ti])
				ti++
			}
		}
	}
	if len(resMap) == 0 {
		if from.Tag != to.Tag {
			return ir.Null().WithTag(mkTagDiff(from.Tag, to.Tag))

		}
		return nil
	}
	res := ir.FromMap(resMap)
	if from.Tag != to.Tag {
		res = res.WithTag(mkTagDiff(from.Tag, to.Tag))
	}
	return res
}

func mkTagDiff(from, to string) string {
	switch {
	case from == "":
		return TagInsertTag + "(" + to[1:] + ")"
	case to == "":
		return TagDeleteTag + "(" + from[1:] + ")"
	default:
		return TagReplaceTag + "(" + from[1:] + "," + to[1:] + ")"
	}
}

func mapFieldsTo(m map[string]rune, im map[rune]string, node *ir.Node) []rune {
	rs := make([]rune, len(node.Fields))
	for i := range node.Fields {
		f := node.Fields[i].String
		r, ok := m[f]
		if !ok {
			r = rune(len(m))
			m[f] = r
			im[r] = f
		}
		rs[i] = r
	}
	return rs
}
