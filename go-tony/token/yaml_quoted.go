package token

import (
	"errors"
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"
)

func YAMLQuotedString(d []byte, pos *Pos) (*Token, int, error) {
	if len(d) == 0 {
		return nil, -1, errors.New("invalid")
	}
	quoteChar := rune(d[0])
	switch quoteChar {
	case '\'':
		return yamlSingleQuoted(d, pos)
	case '"':
		return yamlDoubleQuoted(d, pos)
	default:
		return nil, 0, errors.New("invalid")
	}
}

func yamlSingleQuoted(d []byte, pos *Pos) (*Token, int, error) {
	i := 0
	esc := false
	n := len(d)
	res := []byte{}
Loop:
	for i < n {
		c := d[i]
		i++
		switch c {
		case '\'':
			if i == 1 {
				continue Loop
			}
			if i == 2 {
				esc = true
				goto done
			}

			if esc {
				res = append(res, c)
			}
			esc = !esc
		default:
			if esc {
				i--
				goto done
			}
			res = append(res, c)
		}
	}
done:
	if !esc {
		return nil, 0, fmt.Errorf("%w '", ErrUnterminated)
	}
	return litOrString(res, pos), i, nil
}

func yamlDoubleQuoted(d []byte, pos *Pos) (*Token, int, error) {
	i := 1
	n := len(d)
	dst := []byte{'"'}
	esc := false
	leadingWhite := -1
	for i < n {
		r, sz := utf8.DecodeRune(d[i:])
		i += sz
		switch r {
		case utf8.RuneError:
			return nil, 0, badRune(d[i-sz:])
		case '\r':
			if i > n-1 || d[i+1] != '\n' {
				return nil, 0, ErrYAMLDoubleQuote
			}
			fallthrough
		case '\n':
			if esc {
				leadingWhite = 0
				esc = false
				continue
			}
			esc = false
			if leadingWhite == -1 {
				dst = append(dst, ' ')
				continue
			}
			dst = append(dst, '\n')
			leadingWhite = 0
		case '\\':
			if esc {
				dst = append(dst, '\\', '\\')
				esc = false
				continue
			}
			esc = true
			if leadingWhite != -1 {
				leadingWhite = -1
				continue
			}
		case '"':
			if esc {
				dst = append(dst, '\\', '"')
				esc = false
				continue
			}
			leadingWhite = -1
			dst = append(dst, '"')
			goto done
		default:
			if !esc && leadingWhite == -1 {
				dst = utf8.AppendRune(dst, r)
				continue
			}
			if leadingWhite != -1 {
				if unicode.IsSpace(r) {
					leadingWhite++
					esc = false
					continue
				}
				if leadingWhite == 0 {
					return nil, 0, ErrYAMLDoubleQuote
				}
				leadingWhite = -1
			}
			if !esc {
				dst = utf8.AppendRune(dst, r)
				continue
			}
			esc = false
			switch r {
			case 'n':
				dst = append(dst, '\n')
			case 't':
				dst = append(dst, '\t')
			case 'f':
				dst = append(dst, '\f')
			case 'b':
				dst = append(dst, '\b')
			case 'r':
				dst = append(dst, '\r')
			case 'x', 'u', 'U':
				// A YAML \x, \u or \U escape is a code point in 2, 4 or 8 hex
				// digits. These read the rune from the OUTPUT buffer, never
				// stepped over the digits, and \x ran hex.Encode into a
				// one-byte buffer: `"\u00e9"` panicked with an index out of
				// range, and with a longer prefix decoded to garbage instead.
				width := map[rune]int{'x': 2, 'u': 4, 'U': 8}[r]
				if i+width >= n || !allHex(d[i:i+width]) {
					return nil, 0, ErrBadEscape
				}
				cp, err := strconv.ParseUint(string(d[i:i+width]), 16, 32)
				if err != nil || !utf8.ValidRune(rune(cp)) {
					return nil, 0, ErrBadEscape
				}
				dst = utf8.AppendRune(dst, rune(cp))
				i += width
			case 'N':
				dst = utf8.AppendRune(dst, '\u0085') // next line
			case 'L':
				dst = utf8.AppendRune(dst, '\u2028') // line separator
			case 'P':
				dst = utf8.AppendRune(dst, '\u2029') // paragraph separator
			case ' ':
				dst = append(dst, ' ')
			default:
				return nil, 0, ErrYAMLDoubleQuote
			}
		}
	}
	// The closing quote is the only way out of the loop above, so falling out of
	// it means there was none: unterminated, which the streaming tokenizer reads
	// as "the quote may be in the next read" and a whole document reads as the
	// error it is.  Returned as a complete string, `a: "no closing quote` was
	// accepted with the rest of the line as its value (75g1kbpdh12krs09gdn0).
	return nil, 0, fmt.Errorf(`%w "`, ErrUnterminated)
done:
	return &Token{Type: TString, Pos: pos, Bytes: dst}, i, nil
}
