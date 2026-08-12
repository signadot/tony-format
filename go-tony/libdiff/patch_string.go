package libdiff

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
)

// PatchStringRunes applies a !strdiff(false), whose keys count runes.
func PatchStringRunes(doc, patch *ir.Node) (*ir.Node, error) {
	return patchStringUnits(doc, patch, "rune",
		func(s string) []rune { return []rune(s) },
		func(rs []rune) string { return string(rs) })
}

// PatchStringMultiLine applies a !strdiff(true), whose keys count lines.
func PatchStringMultiLine(doc, patch *ir.Node) (*ir.Node, error) {
	return patchStringUnits(doc, patch, "line",
		func(s string) []string { return strings.Split(s, "\n") },
		func(ls []string) string { return strings.Join(ls, "\n") })
}

// patchStringUnits applies a strdiff to doc a unit at a time, where a unit is
// whatever the diff's keys count: a rune, or a line.  split cuts a string into
// units and join puts them back, each undoing the other exactly.
//
// A key is a position in the sequence the two strings share, which is what di
// walks; fi is where in the document the positions consumed so far leave us.
// See DiffString for what takes a position and why.
func patchStringUnits[U comparable](
	doc, patch *ir.Node,
	unit string,
	split func(string) []U,
	join func([]U) string,
) (*ir.Node, error) {
	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("strdiff only applies to strings, got %s at %s",
			doc.Type, doc.Path())
	}
	diffMap, err := patch.ToIntKeysMap()
	if err != nil {
		return nil, err
	}
	docUnits := split(doc.String)
	res := make([]U, 0, len(docUnits))
	fi, di := uint32(0), uint32(0)
	// A strdiff is relative: it says what changed about the string that was
	// there, so a document which has moved out from under it is a patch which
	// fails, not one which reads off the end of what it was given.
	unexpected := func(key uint32, want string) error {
		have := docUnits[fi:]
		if n := len(split(want)); len(have) > n {
			have = have[:n]
		}
		return fmt.Errorf("at %s cannot patch, unexpected text %q at key %d, expected %q",
			doc.Path(), join(have), key, want)
	}
	for _, key := range slices.Sorted(maps.Keys(diffMap)) {
		op := diffMap[key]
		if key < di {
			return nil, fmt.Errorf(
				"invalid strdiff at %s: key %d is inside the operation before it, which runs to %d",
				patch.Path(), key, di)
		}
		// whatever lies between the operation before and this key is unchanged,
		// and comes from the document
		n := key - di
		if n > uint32(len(docUnits))-fi {
			return nil, fmt.Errorf(
				"invalid strdiff at %s: key %d reaches %s %d of %d",
				patch.Path(), key, unit, fi+n, len(docUnits))
		}
		res = append(res, docUnits[fi:fi+n]...)
		fi += n
		di = key

		tag, _, _ := ir.TagArgs(op.Tag)
		switch tag {
		case DeleteTag:
			if op.Type != ir.StringType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s",
					op.Type, op.Path())
			}
			gone := split(op.String)
			if !hasPrefix(docUnits[fi:], gone) {
				return nil, unexpected(key, op.String)
			}
			fi += uint32(len(gone))
			di += uint32(len(gone))
		case ReplaceTag:
			if op.Type != ir.ObjectType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s",
					op.Type, op.Path())
			}
			to := ir.Get(op, "to")
			if to == nil {
				return nil, fmt.Errorf(
					"invalid strdiff, missing 'to:' under !replace at %s", op.Path())
			}
			from := ir.Get(op, "from")
			if from == nil {
				return nil, fmt.Errorf(
					"invalid strdiff, missing 'from:' under !replace at %s", op.Path())
			}
			was, now := split(from.String), split(to.String)
			if !hasPrefix(docUnits[fi:], was) {
				return nil, unexpected(key, from.String)
			}
			res = append(res, now...)
			fi += uint32(len(was))
			di += uint32(max(len(was), len(now)))
		case InsertTag:
			if op.Type != ir.StringType {
				return nil, fmt.Errorf("invalid strdiff, got type %s at %s",
					op.Type, op.Path())
			}
			added := split(op.String)
			res = append(res, added...)
			di += uint32(len(added))
		default:
			return nil, fmt.Errorf("unexpected tag from strdiff op: %s", tag)
		}
	}
	res = append(res, docUnits[fi:]...)
	return ir.FromString(join(res)), nil
}

func hasPrefix[U comparable](units, prefix []U) bool {
	return len(prefix) <= len(units) && slices.Equal(units[:len(prefix)], prefix)
}
