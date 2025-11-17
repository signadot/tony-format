package libdiff

import (
	"strconv"
	"strings"

	"github.com/signadot/tony-format/tony/ir"

	diffpatch "github.com/sergi/go-diff/diffmatchpatch"
)

func DiffString(from, to *ir.Node) *ir.Node {
	diffCfg := diffpatch.New()
	doMultiLine := strings.Contains(from.String, "\n") && strings.Contains(to.String, "\n")
	diffs := diffCfg.DiffMain(from.String, to.String, doMultiLine)
	diffSize := 0
	resMap := make(map[uint32]*ir.Node)
	ri := uint32(0)
	for i := range diffs {
		diff := &diffs[i]
		//fmt.Printf("ri %d diff op %s\n", ri, diff)
		switch diff.Type {
		case diffpatch.DiffInsert:
			from := resMap[ri]
			to := ir.FromString(diff.Text)
			if from != nil {
				// insert after delete -> make replace
				from.Tag = ""
				resMap[ri] = MakeDiff(from, to)
				if len(diff.Text) > len(from.String) {
					diffSize += len(diff.Text) - len(from.String)
				}
			} else {
				resMap[ri] = to.WithTag(InsertTag)
				diffSize += len(diff.Text)
			}
			ri += uint32(len(diff.Text))
		case diffpatch.DiffDelete:
			resMap[ri] = ir.FromString(diff.Text).WithTag(DeleteTag)
			diffSize += len(diff.Text)
		case diffpatch.DiffEqual:
			ri += uint32(len(diff.Text))
		}
	}
	if diffSize == 0 {
		if from.Tag == to.Tag {
			return nil
		}
		return ir.Null().WithTag(mkTagDiff(from.Tag, to.Tag))
	}
	if diffSize > min(len(from.String), len(to.String))/2 {
		return MakeDiff(from, to)
	}
	res := ir.FromIntKeysMap(resMap)
	tag := ir.TagCompose(StringDiffTag, []string{strconv.FormatBool(doMultiLine)}, "")
	//tag := y.TagCompose(res.Tag, nil, strDiffTag)
	if from.Tag != to.Tag {
		tag = tag + "." + mkTagDiff(from.Tag, to.Tag)[1:]
	} else if from.Tag != "" {
		tag = ir.TagCompose(tag, nil, from.Tag)
	}
	return res.WithTag(tag)
}
