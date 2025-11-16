package token

import (
	"encoding/hex"
	"errors"
	"fmt"
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
			return nil, 0, ErrBadUTF8
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
			case 'x':
				if i+2 >= len(d) {
					return nil, 0, ErrBadEscape
				}
				if !allHex(d[i : i+2]) {
					return nil, 0, ErrBadEscape
				}
				tmp := []byte{0}
				hex.Encode(tmp, d[i:i+2])
				dst = append(dst, tmp[0])
			case 'u':
				if i+4 >= len(d) {
					return nil, 0, ErrBadEscape
				}
				tmp := []byte{0, 0}
				_, err := hex.Decode(tmp, d[i:i+4])
				if err != nil {
					return nil, 0, ErrBadEscape
				}
				r := rune(dst[0])<<8 | rune(dst[1])
				dst = utf8.AppendRune(dst, r)
			case 'U':
				if i+8 >= len(d) {
					return nil, 0, ErrBadEscape
				}
				tmp := []byte{0, 0, 0, 0}
				_, err := hex.Decode(tmp, d[i:i+8])
				if err != nil {
					return nil, 0, ErrBadEscape
				}
				r := rune(dst[0])<<24 | rune(dst[1])<<16 | rune(dst[2])<<8 | rune(dst[3])
				dst = utf8.AppendRune(dst, r)
			case 'N':
				panic("\\N")
			case 'L':
				panic("\\L")
			case ' ':
				dst = append(dst, ' ')
			default:
				return nil, 0, ErrYAMLDoubleQuote
			}
		}
	}
done:
	return &Token{Type: TString, Pos: pos, Bytes: dst}, i, nil
}
