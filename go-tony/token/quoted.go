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

// Unquote decodes a complete quoted string into its value: it validates the whole of v
// with the scanner and only then decodes it. This is the safe entry point for a string of
// unknown provenance — anything that is not exactly one well-formed quoted string is
// reported as an error rather than decoded on a best-effort basis.
//
// The scanner-then-decoder order is what makes it total: bsEscQuoted rejects every input
// that would make the decoder misbehave, and requiring n == len(v) rejects the rest (a
// value like `"a"b"` scans fine but only as its first three bytes).
//
// It previously validated and then returned v unchanged, which meant it never actually
// unquoted anything and had no callers; the three places that needed it had each
// hand-rolled a shape check with no validation, and those reimplementations are where the
// decoder's panics were reachable from stored data.
func Unquote(v string) (string, error) {
	b := []byte(v)
	if len(b) < 2 || (b[0] != '"' && b[0] != '\'') {
		return "", ErrNotQuoted
	}
	n, err := bsEscQuoted(b)
	if err != nil {
		return "", err
	}
	if n != len(v) {
		return "", ErrTrailing
	}
	return quotedToString(b)
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
			// start-sz is the offending byte, not the opening quote: report
			// that so the caller's position points at the actual problem.
			return start - sz, badRune(d[start-sz:])
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

// QuotedToString decodes a complete quoted string token into its value.
//
// It assumes d is exactly one well-formed quoted string, which is what the tokenizer
// produces: the byte range bsEscQuoted returns is always safe to pass here. For a string
// of unknown provenance use Unquote, which validates first and reports what is wrong.
//
// It does not panic. On malformed input it returns the value decoded up to the point of
// the problem, which keeps callers that cannot return an error — Token.String, and so
// every fmt verb that reaches it — from taking down the process over one bad byte.
func QuotedToString(d []byte) string {
	s, _ := quotedToString(d)
	return s
}

// quotedToString is QuotedToString with the diagnosis kept. It returns the value decoded
// so far alongside any error, so callers may use the partial result or discard it.
func quotedToString(d []byte) (string, error) {
	if len(d) == 0 {
		return "", ErrNotQuoted
	}
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
					return b.String(), fmt.Errorf("%w: %q", ErrTrailing, string(d[i:]))
				}
				return b.String(), nil
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
					return b.String(), ErrUnterminated
				}
				dst := []byte{0, 0}
				_, err := hex.Decode(dst, d[i:i+4])
				if err != nil {
					b.WriteRune(utf8.RuneError)
					return b.String(), ErrBadUnicode
				}
				r := rune(dst[0])<<8 | rune(dst[1])
				b.WriteRune(r)
				i += 4
			default:
				// The offending escape only. The previous message sliced a fixed
				// window around it (d[i-sz-4:i+10]) which ran out of bounds on short
				// input, so this arm panicked inside its own panic message and the
				// real diagnosis never reached anyone.
				return b.String(), fmt.Errorf("%w: %q", ErrBadEscape, string(r))
			}
		}
	}
	return b.String(), ErrUnterminated
}
