package libdiff

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/signadot/tony-format/go-tony/ir"

	diffpatch "github.com/sergi/go-diff/diffmatchpatch"
)

// A strdiff key is a position in the sequence the two strings share, counted in
// the unit the diff names: a rune under !strdiff(false), a line under
// !strdiff(true).  Every unit of either string takes one position -- an
// unchanged unit the same position in both, a deleted one the from's alone, an
// inserted one the to's alone -- and a !replace takes as many as its longer
// side.  This is the arraydiff convention of DiffArrayByIndex, counted in units
// rather than in elements, and for the same reason: Reverse swaps from and to
// and rewrites no key, so a position has to mean the same thing read in either
// direction.  A delete which took no position, or a replace which took as many
// as its to: alone, would move under reversal and throw off every key after it.
func DiffString(from, to *ir.Node) *ir.Node {
	multiLine := strings.Contains(from.String, "\n") && strings.Contains(to.String, "\n")
	var chunks []strChunk
	if multiLine {
		// more distinct lines than there are runes to name them with leaves no
		// line diff to be had, so the diff is a rune diff and says so
		chunks, multiLine = lineChunks(from.String, to.String)
	}
	if !multiLine {
		chunks = runeChunks(from.String, to.String)
	}
	resMap, diffSize := strDiffMap(chunks)
	if len(resMap) == 0 {
		if from.Tag == to.Tag {
			return nil
		}
		return ir.Null().WithTag(mkTagDiff(from.Tag, to.Tag))
	}
	if diffSize > min(len(from.String), len(to.String))/2 {
		return MakeDiff(from, to)
	}
	res := ir.FromIntKeysMap(resMap)
	// A strdiff describes the characters of a string, never its tag: an
	// unchanged tag stays on the document and is preserved by the patch.  Only
	// a change to it needs saying.
	tag := ir.TagCompose(StringDiffTag, []string{strconv.FormatBool(multiLine)}, "")
	if from.Tag != to.Tag {
		tag = tag + "." + mkTagDiff(from.Tag, to.Tag)[1:]
	}
	return res.WithTag(tag)
}

// strChunk is one run of the difference between two strings: the text it covers
// in whichever of them has it, how many positions that text takes, and how much
// of the string it accounts for.  A run of lines accounts for the newline after
// each of them as well as the text, which is not in the text itself: a chunk of
// empty lines is not a chunk of nothing.
type strChunk struct {
	op    diffpatch.Operation
	text  string
	units uint32
	size  int
}

// strDiffMap turns the runs of a difference into the operations which state it,
// keyed by position, and reports how much of the string those operations carry
// -- the measure of whether a diff is worth having at all.
func strDiffMap(chunks []strChunk) (map[uint32]*ir.Node, int) {
	resMap := make(map[uint32]*ir.Node, len(chunks))
	diffSize := 0
	ri := uint32(0)
	// a delete with an insert hard against it is one !replace, which takes the
	// positions of its longer side rather than of each side in turn
	var (
		del   *strChunk
		delAt uint32
	)
	for i := range chunks {
		chunk := &chunks[i]
		switch chunk.op {
		case diffpatch.DiffEqual:
			del = nil
			ri += chunk.units
		case diffpatch.DiffDelete:
			resMap[ri] = ir.FromString(chunk.text).WithTag(DeleteTag)
			del, delAt = chunk, ri
			ri += chunk.units
			diffSize += chunk.size
		case diffpatch.DiffInsert:
			if del != nil {
				resMap[delAt] = MakeDiff(
					ir.FromString(del.text), ir.FromString(chunk.text))
				ri = delAt + max(del.units, chunk.units)
				if chunk.size > del.size {
					diffSize += chunk.size - del.size
				}
				del = nil
				continue
			}
			resMap[ri] = ir.FromString(chunk.text).WithTag(InsertTag)
			ri += chunk.units
			diffSize += chunk.size
		}
	}
	return resMap, diffSize
}

// runeChunks diffs from and to a rune at a time.
func runeChunks(from, to string) []strChunk {
	diffs := diffpatch.New().DiffMain(from, to, false)
	chunks := make([]strChunk, len(diffs))
	for i := range diffs {
		chunks[i] = strChunk{
			op:    diffs[i].Type,
			text:  diffs[i].Text,
			units: uint32(utf8.RuneCountInString(diffs[i].Text)),
			size:  len(diffs[i].Text),
		}
	}
	return chunks
}

// lineChunks diffs from and to a line at a time, by naming each distinct line
// with a rune of its own and diffing the two sequences of names.  It reports
// false if there are more distinct lines than there are runes to name them
// with, in which case there is no line diff to be had.
//
// The lines are those of strings.Split, so a trailing newline is a trailing
// empty line and Join puts back exactly what Split took apart.
func lineChunks(from, to string) ([]strChunk, bool) {
	names := map[string]rune{}
	fromLines, toLines := strings.Split(from, "\n"), strings.Split(to, "\n")
	fromNames, ok := lineNames(names, fromLines)
	if !ok {
		return nil, false
	}
	toNames, ok := lineNames(names, toLines)
	if !ok {
		return nil, false
	}
	diffs := diffpatch.New().DiffMainRunes(fromNames, toNames, false)
	chunks := make([]strChunk, len(diffs))
	// which lines a run names is where we have got to in each string, since the
	// runs cover both in order
	fi, ti := 0, 0
	for i := range diffs {
		n := utf8.RuneCountInString(diffs[i].Text)
		var text string
		switch diffs[i].Type {
		case diffpatch.DiffInsert:
			text = strings.Join(toLines[ti:ti+n], "\n")
			ti += n
		case diffpatch.DiffDelete:
			text = strings.Join(fromLines[fi:fi+n], "\n")
			fi += n
		default:
			text = strings.Join(fromLines[fi:fi+n], "\n")
			fi += n
			ti += n
		}
		chunks[i] = strChunk{
			op:    diffs[i].Type,
			text:  text,
			units: uint32(n),
			size:  len(text) + n,
		}
	}
	return chunks, true
}

// lineNames gives each distinct line a rune of its own, so that a sequence of
// lines can be diffed as if it were a string.  The surrogate range is stepped
// over: diffmatchpatch hands the names back as a string, which would turn a
// surrogate into U+FFFD and make two different lines look like one.
func lineNames(names map[string]rune, lines []string) ([]rune, bool) {
	rs := make([]rune, len(lines))
	for i, line := range lines {
		r, ok := names[line]
		if !ok {
			r = rune(len(names))
			if r >= 0xd800 {
				r += 0xe000 - 0xd800
			}
			if r > utf8.MaxRune {
				return nil, false
			}
			names[line] = r
		}
		rs[i] = r
	}
	return rs, true
}
