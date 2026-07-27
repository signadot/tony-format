package libdiff

import (
	"strconv"

	"github.com/signadot/tony-format/go-tony/ir"

	diffpatch "github.com/sergi/go-diff/diffmatchpatch"
)

// 1 diff field names
// for every different field name add  node
// for every same field name, recurse on the value
func DiffObject(from, to *ir.Node, df DiffFunc) *ir.Node {
	fromSparse := ir.TagHas(from.Tag, ir.IntKeysTag)
	toSparse := ir.TagHas(to.Tag, ir.IntKeysTag)
	if fromSparse != toSparse {
		return to
	}
	fieldMap := map[string]rune{}
	runeMap := map[rune]string{}
	fromRunes := mapFieldsTo(fieldMap, runeMap, from)
	toRunes := mapFieldsTo(fieldMap, runeMap, to)
	diffCfg := diffpatch.New()
	diffs := diffCfg.DiffMainRunes(fromRunes, toRunes, false)
	// The sequence of field names is diffed to find which fields each side
	// has, but the result is keyed by name rather than by position, and a
	// reordering shows up as a field deleted here and inserted there.  Both
	// land on the same key, so collect where each name occurs on each side
	// first: a name on both sides is a field which changed value, whatever the
	// sequence diff made of the move.
	type sides struct{ fromIdx, toIdx int }
	occurs := make(map[string]*sides, len(fieldMap))
	at := func(name string) *sides {
		s := occurs[name]
		if s == nil {
			s = &sides{fromIdx: -1, toIdx: -1}
			occurs[name] = s
		}
		return s
	}
	fi, ti := 0, 0
	for i := range diffs {
		diff := &diffs[i]
		switch diff.Type {
		case diffpatch.DiffDelete:
			for _, r := range diff.Text {
				at(runeMap[r]).fromIdx = fi
				fi++
			}
		case diffpatch.DiffEqual:
			for _, r := range diff.Text {
				s := at(runeMap[r])
				s.fromIdx, s.toIdx = fi, ti
				fi++
				ti++
			}
		case diffpatch.DiffInsert:
			for _, r := range diff.Text {
				at(runeMap[r]).toIdx = ti
				ti++
			}
		}
	}
	resMap := map[string]*ir.Node{}
	for name, s := range occurs {
		switch {
		case s.fromIdx >= 0 && s.toIdx >= 0:
			if fRes := df(from.Values[s.fromIdx], to.Values[s.toIdx]); fRes != nil {
				resMap[name] = fRes
			}
		case s.fromIdx >= 0:
			resMap[name] = MakeDiff(from.Values[s.fromIdx], nil)
		default:
			resMap[name] = MakeDiff(nil, to.Values[s.toIdx])
		}
	}
	if len(resMap) == 0 {
		if from.Tag != to.Tag {
			return ir.Null().WithTag(mkTagDiff(from.Tag, to.Tag))

		}
		return nil
	}
	if !fromSparse {
		res := ir.FromMap(resMap)
		if from.Tag != to.Tag {
			res = res.WithTag(mkTagDiff(from.Tag, to.Tag))
		}
		return res
	}
	ikMap := make(map[uint32]*ir.Node, len(resMap))
	for k, v := range resMap {
		ik, err := strconv.ParseUint(k, 10, 32)
		if err != nil {
			panic(err)
		}
		ikMap[uint32(ik)] = v
	}
	res := ir.FromIntKeysMap(ikMap)
	if from.Tag != to.Tag {
		res = res.WithTag(ir.TagCompose(res.Tag, nil, mkTagDiff(from.Tag, to.Tag)))
	} else {
		res.Tag = from.Tag
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
