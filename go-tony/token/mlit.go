package token

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MLitChomp = '-'
	MLitKeep  = '+'
)

func mLitIndent(toks []Token, d int) (int, error) {
	n := len(toks)
	if n == 0 {
		if d == 0 {
			return 2, nil
		}
		return 0, nil
	}
	last := &toks[n-1]
	switch last.Type {
	case TIndent:
		if d == 0 {
			return len(last.Bytes) + 2, nil
		}
		return len(last.Bytes), nil
	case TTag:
		return mLitIndent(toks[:n-1], d)
	case TArrayElt:
		res, err := mLitIndent(toks[:n-1], d+1)
		if err != nil {
			return 0, err
		}
		return res + 2, nil
	case TColon:
		if n < 2 {
			return 0, ErrMLitPlacement
		}
		res, err := mLitIndent(toks[:n-2], d+1)
		if err != nil {
			return 0, err
		}
		return res + 2, nil
	default:
		return 0, ErrMLitPlacement
	}
}

// openLine reads a block literal's opening line: the '|', an optional chomp
// indicator, and then what any other line may carry after its content --
// trailing whitespace, and a comment.
//
// A line ends at its last non-space character or at a comment, and a block
// literal's opening line is a line. It used to be the one place in the format
// which was strict about this, and strict about the wrong thing: `| ` and
// `| # why` were refused where `k: v ` and `k: v # why` are accepted, and the
// refusal came out as `unexpected ""`, which tells a reader nothing
// (0y342gdzh12ks0vkgxn0, 6ykv73beh12krzeygsn0). docs/tony.md's own leading-space
// example is written `| ` and did not parse. YAML mode inherited the refusal,
// where PyYAML accepts both.
//
// A comment there is a LINE comment on the literal, the same as `k: v # why`.
// It is not a head comment on the content: a head comment can already be written
// on the line above, and both can be written at once
//
//	# what the value is
//	| # how it is written
//	  ...
//
// so reading the second as a head comment would merge the two and lose a position
// the format lets a writer use.
//
// It answers the chomp indicator, the offset the content starts at (just past the
// newline), the offset of the newline itself, the offset of the comment's '#' or
// -1 when there is none, and bad: -1 when the line is well formed, len(d) when it
// runs out before its newline, and otherwise the offset of the byte which may not
// be there.
func openLine(d []byte) (chomp byte, start, nl, cmt, bad int) {
	chomp, i := byte('\n'), 1
	if len(d) > 1 && (d[1] == MLitChomp || d[1] == MLitKeep) {
		chomp, i = d[1], 2
	}
	// \r is skipped with the rest, so a CRLF document opens a literal like any
	// other: `|\r\n` was refused too, for the same reason.
	for i < len(d) && (d[i] == ' ' || d[i] == '\t' || d[i] == '\r') {
		i++
	}
	cmt = -1
	if i < len(d) && d[i] == '#' {
		cmt = i
		for i < len(d) && d[i] != '\n' {
			i++
		}
	}
	switch {
	case i >= len(d):
		return chomp, 0, 0, cmt, len(d)
	case d[i] != '\n':
		return chomp, 0, 0, cmt, i
	}
	return chomp, i + 1, i, cmt, -1
}

func mLit(d []byte, indent int, posDoc *PosDoc, off int) (int, error) {
	if len(d) < 2 {
		return 0, ErrUnterminated
	}
	if d[0] != '|' {
		return 0, fmt.Errorf("unexpected %q", string(d[0]))
	}
	format, start, nl, _, bad := openLine(d)
	switch {
	case bad == len(d):
		return 0, ErrUnterminated
	case bad >= 0:
		return 0, UnexpectedErr(string(d[bad]), posDoc.Pos(off+bad))
	}
	posDoc.nl(off + nl)
	rest, err := scanLines(d[start:], posDoc, off+start, indent, format)
	if err != nil {
		return 0, err
	}
	return start + rest, nil
}

func mLitToString(d []byte) string {
	u, sz := binary.Uvarint(d)
	if sz <= 0 {
		panic(sz)
	}
	theIndent := int(u)
	if theIndent <= 0 {
		panic(theIndent)
	}
	d = d[sz:]
	if len(d) < 2 {
		return ""
	}
	chomp, i, _, _, _ := openLine(d)
	b := &strings.Builder{}
	trailing := 0
	for i < len(d) {
		indent := readIndent(d[i:])
		if indent == 0 { //&& d[i] == '\n' {
			b.WriteByte('\n')
			i++
			trailing++
			continue
		}
		trailing = 0

		if indent < theIndent {
			break
		}
		j := i + indent
		for j < len(d) {
			if d[j] == '\n' {
				j++
				break
			}
			j++
		}
		b.Write(d[i+theIndent : j])
		i = j
	}
	res := b.String()
	if chomp == MLitKeep {
		return res
	}

	if chomp == MLitChomp {
		trailing++
	}
	end := len(res) - trailing
	if end < 0 {
		end = 0
	}
	return res[:end]
}

func scanLines(d []byte, posDoc *PosDoc, off, indent int, format byte) (int, error) {
	i := 0
	n := len(d)
	for i < n {
		end, lineSz, err := scanLine(d[i:], indent)
		if err != nil {
			return 0, err
		}
		if end {
			break
		}
		i += lineSz
		posDoc.nl(i + off - 1)
	}
	if i == 0 {
		if n == 0 {
			// Nothing follows the `|` line at all: an empty literal, which is what
			// the document says.  This was only ever reached with a newline the
			// reader appended for itself, and that newline grew keep-chomped
			// values on every round trip.
			return 0, nil
		}
		// The first line is not indented enough to be content, so the literal has
		// none. Say what was wanted: the indent is computed, not detected, so a
		// reader who guessed wrong has no way to see the number otherwise, and
		// each '- ' on the opening line adds one level to it.
		e := fmt.Errorf("%w: its content starts at column %d and must start at %d",
			ErrMalformedMLit, readIndent(d), indent)
		return 0, NewTokenizeErr(e, posDoc.Pos(off))
	}
	if d[i-1] != '\n' {
		return 0, NewTokenizeErr(ErrMalformedMLit, posDoc.Pos(off+i))
	}
	if format != MLitKeep {
		return i, nil
	}
	trailing := i
	trailIndent := 0
	for trailing < n {
		c := d[trailing]
		trailing++
		switch c {
		case '\r':
		case '\n':
			posDoc.nl(off + trailing - 1)
			i = trailing - 1
			trailIndent = 0
		case ' ':
			trailIndent++
			if trailIndent > indent {
				e := fmt.Errorf("%w: indent %d > %d", ErrMalformedMLit,
					trailIndent, indent)
				return 0, NewTokenizeErr(e, posDoc.Pos(off+i))
			}
		default:
			goto done
		}
	}
done:
	return i, nil
}

func readIndent(d []byte) int {
	i := 0
	n := len(d)
	for i < n {
		c := d[i]
		switch c {
		case ' ':
		default:
			return i
		}
		i++
	}
	return i
}

func scanLine(d []byte, indent int) (bool, int, error) {
	n := len(d)
	i := 0
	leading := 0
	nonIndent := false
	for i < n {
		r, sz := utf8.DecodeRune(d[i:])
		i += sz
		switch r {
		case utf8.RuneError:
			return false, 0, badRune(d[i-sz:])
		case '\n':
			if leading >= indent {
				return false, i, nil
			}
			if i == 1 {
				return false, i, nil
			}
			return true, i, nil
		case ' ':
			if !nonIndent {
				leading++
			}
		default:
			nonIndent = true
		}
	}
	return false, i, nil
}
