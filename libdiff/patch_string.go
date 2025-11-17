package libdiff

import (
	"fmt"
	"strings"

	"github.com/signadot/tony-format/tony/ir"
)

func PatchStringRunes(doc, patch *ir.Node) (*ir.Node, error) {
	diffMap, err := patch.ToIntKeysMap()
	if err != nil {
		return nil, err
	}
	res := []rune{}

	txt := []rune(doc.String)
	fi, di := uint32(0), uint32(0)
	n := uint32(len(txt))
	diffCount := 0
	for diffCount <= len(diffMap) {
		op := diffMap[di]
		//fmt.Printf("n = %d; fi = %d; di = %d; op = %v\n", len(res), fi, di, op)
		if op == nil {
			if diffCount == len(diffMap) {
				if fi < n {
					res = append(res, txt[fi:]...)
				}
				break
			}
			if fi < n {
				res = append(res, txt[fi])
				fi++
			}
			di++
			continue
		}
		diffCount++
		tag, _, _ := ir.TagArgs(op.Tag)
		switch tag {
		case "!delete":
			if op.Type != ir.StringType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s", op.Type, op.Path())
			}
			if !runesHasPrefix(txt[fi:], op.String) {
				return nil, fmt.Errorf("at %s cannot patch, unexpected text %q, expected %q", doc.Path(), string(txt[fi:]), op.String)
			}
			for range op.String {
				fi++
			}
			di++
		case "!replace":
			if op.Type != ir.ObjectType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s", op.Type, op.Path())
			}
			to := ir.Get(op, "to")
			if to == nil {
				return nil, fmt.Errorf("invalid strdiff, missing 'to:' under !replace at %s", op.Path())
			}
			from := ir.Get(op, "from")
			if from == nil {
				return nil, fmt.Errorf("invalid strdiff, missing 'from:' under !replace at %s", op.Path())
			}
			if !runesHasPrefix(txt[fi:], from.String) {
				return nil, fmt.Errorf("cannot patch, unexpected text %q, expected %q", string(txt[fi:]), to.String)
			}
			add := []rune(to.String)
			res = append(res, add...)
			di += uint32(len(add))
			for range from.String {
				fi++
			}
		case "!insert":
			if op.Type != ir.StringType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s", op.Type, op.Path())
			}
			add := []rune(op.String)
			res = append(res, add...)
			di += uint32(len(add))
		default:
			return nil, fmt.Errorf("unexpected tag from strdiff op: %s", tag)
		}
	}
	return ir.FromString(string(res)), nil
}

func runesHasPrefix(txt []rune, str string) bool {
	n := 0
	for _, r := range str {
		if n >= len(txt) {
			return true
		}
		if txt[n] != r {
			return false
		}
		n++
	}
	return true
}

func PatchStringMultiLine(doc, patch *ir.Node) (*ir.Node, error) {
	docLines := strings.Split(doc.String, "\n")
	diffMap, err := patch.ToIntKeysMap()
	if err != nil {
		return nil, err
	}
	resLines := make([]string, 0, len(docLines)+len(diffMap))
	fi, di := uint32(0), uint32(0)
	diffCount := 0
	for diffCount <= len(diffMap) {
		op := diffMap[di]
		if op == nil {
			if diffCount == len(diffMap) {
				resLines = append(resLines, docLines[fi:]...)
				break
			}
			resLines = append(resLines, docLines[fi])
			fi++
			di++
			continue
		}
		diffCount++
		tag, _, _ := ir.TagArgs(op.Tag)
		switch tag {
		case "!delete":
			if op.Type != ir.StringType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s", op.Type, op.Path())
			}
			opLines := strings.Split(op.String, "\n")
			if !linesHasPrefix(docLines[fi:], opLines) {
				return nil, fmt.Errorf("unexpected")
			}
			fi += uint32(len(opLines))
			di++
		case "!replace":
			if op.Type != ir.ObjectType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s", op.Type, op.Path())
			}
			to := ir.Get(op, "to")
			if to == nil {
				return nil, fmt.Errorf("invalid strdiff, missing 'to:' under !replace at %s", op.Path())
			}
			toLines := strings.Split(to.String, "\n")
			from := ir.Get(op, "from")
			if from == nil {
				return nil, fmt.Errorf("invalid strdiff, missing 'from:' under !replace at %s", op.Path())
			}
			fromLines := strings.Split(from.String, "\n")
			if !linesHasPrefix(docLines[fi:], fromLines) {
				return nil, fmt.Errorf("cannot patch, unexpected text %q, expected %q", string(docLines[fi]), fromLines[0])
			}
			resLines = append(resLines, toLines...)
			di += uint32(len(toLines))
			fi += uint32(len(fromLines))
		case "!insert":
			if op.Type != ir.StringType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s", op.Type, op.Path())
			}
			opLines := strings.Split(op.String, "\n")
			resLines = append(resLines, opLines...)
			di += uint32(len(opLines))
		default:
			return nil, fmt.Errorf("unexpected tag from strdiff op: %s", tag)
		}
	}
	return ir.FromString(strings.Join(resLines, "\n")), nil
}

func linesHasPrefix(lines []string, prefix []string) bool {
	n := 0
	for i, line := range lines {
		if i == len(prefix) {
			return true
		}
		if line != prefix[i] {
			return false
		}
		n++
	}
	return n == len(prefix)
}
