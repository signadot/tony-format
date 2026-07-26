package token

import (
	"errors"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"
)

// getSingleLiteralStreaming is a streaming-aware getSingleLiteral for use by the
// tokenizer: when the literal's scan runs to the end of the buffer (so it may
// continue in the next read), it returns io.EOF instead of a possibly-truncated
// literal, and the source grows the buffer and retries. In-memory callers
// (litOrString, NeedsQuote) keep using getSingleLiteral, where reaching the end
// is legitimate.
//
// getSingleLiteral trims a trailing ':' or ',', which hides the raw scan end, so
// that trimmed separator is added back when checking against the buffer end (a
// literal like "foo:bar" split as "foo:" | "bar" must not be emitted as "foo").
func getSingleLiteralStreaming(d []byte) ([]byte, error) {
	lit, err := getSingleLiteral(d)
	if err != nil {
		// A rune cut off by the end of the buffer is the same situation as a
		// literal that runs to the end: the scan needs the next read, not a
		// verdict.
		if errors.Is(err, ErrPartialRune) {
			return nil, io.EOF
		}
		return nil, err
	}
	end := len(lit)
	if end < len(d) && (d[end] == ':' || d[end] == ',') {
		end++ // account for the trimmed trailing separator
	}
	if end == len(d) {
		return nil, io.EOF
	}
	return lit, nil
}

func getSingleLiteral(d []byte) ([]byte, error) {
	var (
		i          = 0
		n          = len(d)
		r          rune
		sz         int
		cb, sb, rb = 0, 0, 0
	)
	for i < n {
		r, sz = utf8.DecodeRune(d[i:])
		if r == utf8.RuneError {
			return nil, badRune(d[i:])
		}
		if unicode.IsSpace(r) {
			break
		}
		if unicode.IsControl(r) {
			break
		}
		switch r {
		case '(':
			rb++
		case ')':
			rb--
			if rb < 0 {
				goto done
			}
		case '[':
			sb++
		case ']':
			sb--
			if sb < 0 {
				goto done
			}
		case '{':
			cb++
		case '}':
			cb--
			if cb < 0 {
				goto done
			}

		case '$', '~', '@', ':', '/', '.', '_', '+', '-', '\\', '*', '%', '!', '=':
		default:
			if unicode.IsPunct(r) {
				goto done
			}
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsDigit(r) && !unicode.IsGraphic(r) && !unicode.IsSymbol(r) {
				goto done
			}
		}

		i += sz
	}
done:
	if i > 0 {
		// no leading :
		switch d[0] {
		case ':', '[', '!', '{':
			return nil, fmt.Errorf("%w: invalid leading character %c", ErrLiteral, d[0])
		}
	}
	if cb > 0 {
		return nil, fmt.Errorf("%w: unbalanced {}", ErrLiteral)
	}
	if sb > 0 {
		return nil, fmt.Errorf("%w: unbalanced [] (%d)", ErrLiteral, sb)
	}
	if cb > 0 {
		return nil, fmt.Errorf("%w: unbalanced [] (%d)", ErrLiteral, sb)
	}
	// chop trailing colon, comma
	if i > 0 {
		c := d[i-1]
		switch c {
		case ':', ',':
			i--
		default:
			break
		}
	}

	if i == 0 {
		return nil, ErrLiteral
	}
	return d[0:i], nil
}

func isMidLiteral(r rune) bool {
	switch r {
	case '$', '~', '@', ':', '/', '.', '_', '+', '-', '\\', '*', '%', '!', '=', '[', ']':
		return true
	case utf8.RuneError:
		return false
	}
	if unicode.IsPunct(r) || unicode.IsControl(r) || unicode.IsSpace(r) {
		return false
	}
	if unicode.IsDigit(r) || unicode.IsLetter(r) || unicode.IsGraphic(r) {
		return true
	}
	return false
}

func litOrString(d []byte, pos *Pos) *Token {
	litBytes, err := getSingleLiteral(d)
	if err != nil || len(litBytes) != len(d) {
		// represent as string with quoting
		return &Token{
			Type:  TString,
			Pos:   pos,
			Bytes: []byte(Quote(string(d), true)),
		}
	}
	return &Token{
		Type:  TLiteral,
		Pos:   pos,
		Bytes: litBytes,
	}
}
