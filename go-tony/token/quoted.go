package token

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func NeedsQuote(v string) bool {
	if v == "" {
		return true
	}
	d, err := getSingleLiteral([]byte(v))
	if err != nil {
		return true
	}
	if len(d) != len(v) {
		// chopped
		return true
	}
	switch v[0] {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	switch v {
	case "true", "false", "null":
		return true
	default:
		return false
	}
}

// KPathQuoteField returns true if a field name needs to be quoted in a kinded path.
// A field needs quoting if:
//   - It contains characters that require quoting according to NeedsQuote (spaces, special chars)
//   - It contains any of the path syntax characters: ".", "[", "{"
func KPathQuoteField(v string) bool {
	return NeedsQuote(v) || strings.ContainsAny(v, ".[{")
}

func Quote(v string, autoSingle bool) string {
	n := len(v)
	ndq, nsq := 0, 0
	d := make([]byte, 1, len(v)+2)
	d[0] = '"'
	ucs := []byte{0, 0}
	cps := []byte{0, 0, 0, 0}
	for _, r := range v {
		switch r {
		case '"':
			ndq++
			d = append(d, '\\', '"')
		case '\'':
			nsq++
			d = append(d, '\'')
		case '\\':
			d = append(d, '\\', '\\')
		case '\b':
			d = append(d, '\\', 'b')
		case '\f':
			d = append(d, '\\', 'f')
		case '\n':
			d = append(d, '\\', 'n')
		case '\r':
			d = append(d, '\\', 'r')
		case '\t':
			d = append(d, '\\', 't')
		default:
			if unicode.IsControl(r) {
				ucs[0] = byte(r >> 8)
				ucs[1] = byte(r)
				cps = hex.AppendEncode(cps[:0], ucs)
				d = append(d, '\\', 'u', cps[0], cps[1], cps[2], cps[3])
			} else {
				d = utf8.AppendRune(d, r)
			}
		}
	}
	d = append(d, '"')
	if !autoSingle || nsq >= ndq {
		return string(d)
	}
	n = len(d)
	sd := make([]byte, 0, n)
	j := 0
	for i, c := range d {
		switch c {
		case '\'':
			sd = append(sd, '\\', '\'')
			j += 2
		case '"':
			switch i {
			case 0:
				sd = append(sd, '\'')
				j++
			case n - 1:
				sd = append(sd, '\'')
				j++
			default:
				// it was quoted, overwrite \
				sd[j-1] = '"'
			}
		default:
			sd = append(sd, c)
			j++
		}
	}
	return string(sd)
}

func Unquote(v string) (string, error) {
	b := []byte(v)
	n, err := bsEscQuoted(b)
	if err != nil {
		return "", err
	}
	if n != len(v) {
		return "", ErrUnterminated
	}
	return string(b), nil
}

func bsEscQuoted(d []byte) (int, error) {
	if len(d) == 0 {
		return -1, errors.New("invalid")
	}
	quoteChar := rune(d[0])
	escaped := false
	start := 1
	n := len(d)
	for start < n {
		r, sz := utf8.DecodeRune(d[start:])
		start += sz
		switch r {
		case utf8.RuneError:
			return 0, ErrBadUTF8
		case quoteChar:
			if !escaped {
				return start, nil
			}
			escaped = false
		case 'u':
			if escaped {
				if start+4 > n {
					return start, ErrUnterminated
				}
				if !allHex(d[start : start+4]) {
					return start, ErrBadUnicode
				}
			}
			escaped = false
		case '/', 'b', 'f', 'n', 'r', 't':
			escaped = false
		case '\\':
			escaped = !escaped
		default:
			if unicode.IsControl(r) {
				return start, ErrUnicodeControl
			}
			if escaped {
				return start, ErrBadEscape
			}
			escaped = false
		}
	}
	return 0, ErrUnterminated
}

func allHex(d []byte) bool {
	for _, c := range d {
		if c >= '0' && c <= '9' {
			continue
		}
		if c >= 'a' && c <= 'f' {
			continue
		}
		if c >= 'A' && c <= 'F' {
			continue
		}
		return false
	}
	return true
}

func QuotedToString(d []byte) string {
	qc := rune(d[0])
	b := &strings.Builder{}
	i := 1
	esc := false
	for i < len(d) {
		r, sz := utf8.DecodeRune(d[i:])
		i += sz
		switch r {
		case '\\':
			if esc {
				b.WriteByte(byte(r))
			}
			esc = !esc
		case qc:
			if !esc {
				if i != len(d) {
					panic(fmt.Sprintf("internal string: trailing %q", string(d[i:])))
				}
				return b.String()
			}
			b.WriteRune(qc)
			esc = false
		default:
			if !esc {
				b.WriteRune(r)
				continue
			}
			esc = false
			switch r {
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'f':
				b.WriteByte('\f')
			case 'r':
				b.WriteByte('\r')
			case '/':
				b.WriteByte('/')
			case 'b':
				b.WriteByte('\b')
			case 'u':
				if i >= len(d) || len(d[i:]) < 4 {
					b.WriteRune(utf8.RuneError)
					return b.String()
				}
				dst := []byte{0, 0}
				_, err := hex.Decode(dst, d[i:i+4])
				if err != nil {
					b.WriteRune(utf8.RuneError)
					return b.String()
				}
				r := rune(dst[0])<<8 | rune(dst[1])
				b.WriteRune(r)
				i += 4
			default:
				panic(fmt.Sprintf("internal string %q", string(d[i-sz-4:i+10])))
			}
		}
	}
	return b.String()
}
