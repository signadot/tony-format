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

func mLit(d []byte, indent int, posDoc *PosDoc, off int) (int, error) {
	if len(d) < 2 {
		return 0, ErrUnterminated
	}
	if d[0] != '|' {
		return 0, fmt.Errorf("unexpected %q", string(d[0]))
	}
	start := 2
	format := d[1]
	switch format {
	case MLitChomp, MLitKeep:
		if len(d) < 3 {
			return 0, ErrUnterminated
		}
		start++
		posDoc.nl(off + 2)
	case '\n':
		posDoc.nl(off + 1)
	default:
		return 0, UnexpectedErr(string(format), posDoc.Pos(off+1))
	}
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
	i := 1
	if d[1] != '\n' {
		i++
	}
	i++ // initial \n
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
	if d[1] == '+' {
		return res
	}

	if d[1] == '-' {
		trailing++
	}
	return res[:len(res)-trailing]
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
		return 0, NewTokenizeErr(ErrMalformedMLit, posDoc.Pos(off))
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
			return false, 0, ErrBadUTF8
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
