package token

import (
	"bytes"
	"errors"
	"io"
)

// digitLeadingLiteral returns the maximal literal run at the start of d when that run is
// longer than the numLen-byte number lexed at the same place, i.e. when the scalar began
// with a digit but did not finish as a number: "30s", "1Gi", "1.2.3", "3.".
//
// It returns nil when the run is no longer than the number, which is the ordinary case of
// a scalar that is simply a number. It also returns nil when there is no literal run at
// all — "1[2" leaves getSingleLiteral with an unbalanced '[', and that stays the number
// followed by a bracket, as before.
//
// The run stops at the first ':'. getSingleLiteral only chops a ':' at the very end, so
// "0:42" — a sparse-array key written without a space after the colon — comes back whole
// and would otherwise read as a digit-leading scalar. A number cannot contain ':', so
// within a digit-leading run the colon is always the key separator.
//
// io.EOF means the run reached the end of the buffer, so whether it extends past the
// number cannot be decided yet and the source must read more. numberStreaming has already
// established that the number itself is fully buffered; the literal may still continue,
// as in a chunk ending "...30s" with more "s" to come. At true EOF the trailing newline
// ends the run, so this does not loop.
func digitLeadingLiteral(d []byte, numLen int) ([]byte, error) {
	if numLen < len(d) && endsNumber(d[numLen]) {
		// Fast path: the byte after the number already ends the literal run, so the
		// run and the number are the same and there is nothing to scan. This is every
		// well-formed number, which is why it is worth not scanning twice for them.
		return nil, nil
	}
	lit, err := getSingleLiteralStreaming(d)
	if err != nil {
		if errors.Is(err, ErrLiteral) {
			return nil, nil
		}
		return nil, err
	}
	if i := bytes.IndexByte(lit, ':'); i >= 0 {
		lit = lit[:i]
	}
	if len(lit) <= numLen {
		return nil, nil
	}
	return lit, nil
}

// endsNumber reports whether c terminates a literal run, so that a number followed by it
// is the whole scalar. Every byte here is one getSingleLiteral stops at: white space and
// control characters, punctuation such as ',' '#' and the quotes, and the closers, which
// are unopened at this point since the run began with a digit. ':' is included because
// digitLeadingLiteral truncates the run there in any case.
//
// Anything not listed falls through to the full scan, so a byte missing from this set
// costs a little time and never a wrong answer.
func endsNumber(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ',', ':', '#', '"', '\'', ')', ']', '}':
		return true
	default:
		return false
	}
}

// numberStreaming is number() with read-buffer-boundary awareness for the
// streaming tokenizer. number() (via asciiDigits/fract/exp) treats the end of the
// slice as the end of the token, so a number whose digits/fraction/exponent run
// to the buffer end is under-parsed (e.g. "3.14159" without a terminator parses
// as the integer 3, and "10000000005" that continues is truncated). To avoid
// that, this scans the maximal run of number characters: if that run reaches the
// end of d the number may continue, so it returns io.EOF and the source grows the
// buffer and retries; otherwise a terminator is present and number() parses the
// fully-buffered token correctly. At true EOF a trailing newline terminates the
// run, so this does not loop.
func numberStreaming(d []byte) (int, bool, error) {
	i := 0
	for i < len(d) {
		c := d[i]
		switch {
		case asciiDigit(c) || c == '.':
			i++
		case c == 'e' || c == 'E':
			i++
			if i < len(d) && (d[i] == '+' || d[i] == '-') {
				i++
			}
		default:
			return number(d) // terminator in view: the whole number is buffered
		}
	}
	return 0, false, io.EOF // number characters run to the buffer end; need more
}

func number(d []byte) (int, bool, error) {
	digits := asciiDigits(d)
	if digits == 0 {
		return 0, false, ErrNumber
	}
	f := fract(d[digits:])
	e := exp(d[digits+f:])
	if f+e == 0 {
		if digits > 1 && d[0] == '0' {
			return digits, false, ErrNumberLeadingZero
		}
		return digits, false, nil
	}
	return f + e + digits, true, nil
}

func asciiDigits(d []byte) int {
	i := 0
	for i < len(d) {
		if !asciiDigit(d[i]) {
			return i
		}
		i++
	}
	return i
}

func asciiDigit(c byte) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	default:
		return false
	}
}

func exp(d []byte) int {
	if len(d) < 2 {
		return 0
	}
	switch d[0] {
	case 'e', 'E':
	default:
		return 0
	}
	i := 1
	switch d[1] {
	case '+', '-':
		i++
	default:
	}
	if i == len(d) {
		return 0
	}
	n := asciiDigits(d[i:])
	if n == 0 {
		return 0
	}
	return n + i
}

func fract(d []byte) int {
	if len(d) == 0 {
		return 0
	}
	if d[0] != '.' {
		return 0
	}
	for i := 1; i < len(d); i++ {
		if !asciiDigit(d[i]) {
			if i == 1 {
				// . must be followed by 1 or more digits rfc 7159
				return 0
			}
			return i
		}
	}
	return 0
}
