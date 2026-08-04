package token

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"unicode"

	"github.com/signadot/tony-format/go-tony/format"
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

// digitLeadingToken builds the literal token for a digit-leading run that reads as a
// string, reporting false when the run is a botched number and the caller should raise
// ErrDigitLeading instead.
//
// Only the tony format accepts these.  JSON has no unquoted scalars at all, so a run that
// outruns its number there is an error whatever shape it has.  YAML never reaches here:
// its plain-scalar scanner handles the same ground earlier in TokenizeOne.
func (t *Tokenizer) digitLeadingToken(lit []byte, absOffset int) (Token, bool) {
	if t.opt.format != format.TonyFormat {
		return Token{}, false
	}
	typ := TokenType(TLiteral)
	switch {
	case radixOK(lit):
		// A radix literal is a number, so it is claimed before the string rules
		// get to it -- "0x1f" holds a letter, and reading it as the text "0x1f"
		// rather than as 31 is the misreading all of this is arranged to avoid.
		typ = TInteger
	case digitLeadingString(lit):
	default:
		return Token{}, false
	}
	t.ts.hasValue = true
	return Token{
		Type:  typ,
		Pos:   t.posDoc.Pos(absOffset),
		Bytes: lit,
	}, true
}

func radixOK(lit []byte) bool {
	_, _, ok := RadixLiteral(string(lit))
	return ok
}

// digitLeadingString reports whether a digit-leading literal run reads as a string rather
// than as a botched number.  It is the rule the tokenizer applies once a run has outrun
// the number inside it, and NeedsQuote applies the same rule in reverse.
//
// Two shapes qualify.  A run containing a letter is a quantity or a duration -- "100m",
// "1Gi", "30s", "1h30m" -- which is the shape Kubernetes manifests are full of and the
// reason any of this exists.  A run of three or more dot-separated digit groups is a
// version or an address -- "1.2.3", "192.168.1.1".  Two groups is a float and never
// reaches here.
//
// Everything else stays an error, which keeps a mistyped number loud: "1_000", "3..14",
// "1." and "1e+" are all typing accidents rather than text, and reading them as strings
// would hide that.
//
// A run holding ':' is not a string either.  The tokenizer stops a digit-leading run at
// the first colon, so it can never produce one; the check is here for NeedsQuote, which is
// handed a whole string and would otherwise call "30s:x" safe to write bare, where it
// would read back as the key "30s".
//
// Radix prefixes are excluded for now.  "0x1f" means 31 to everyone who writes it, so
// reading it as text would be exactly the silent misreading this rule set exists to avoid.
// It stays an error until number() learns the notation, so the value only ever moves from
// error to number and never quietly changes meaning.
func digitLeadingString(lit []byte) bool {
	if bytes.IndexByte(lit, ':') >= 0 {
		return false
	}
	d := lit
	if len(d) > 0 && d[0] == '-' {
		d = d[1:]
	}
	if len(d) == 0 || radixPrefixed(d) {
		return false
	}
	if hasLetter(d) {
		return true
	}
	return dottedDigits(d)
}

// digitLeadingNeedsQuote reports whether a string starting with a digit, or with '-' and a
// digit, has to be quoted to come back as itself.  A run that is entirely a number reads
// back as that number rather than as text, so "1e9" and "-1" need quoting where "100m"
// does not.
func digitLeadingNeedsQuote(v string) bool {
	d := []byte(v)
	off := 0
	if d[0] == '-' {
		off = 1
	}
	if numLen, _, err := number(d[off:]); err == nil && numLen+off == len(d) {
		return true
	}
	return !digitLeadingString(d)
}

// radixPrefixed reports whether d opens with a hexadecimal, binary or octal prefix.
func radixPrefixed(d []byte) bool {
	if len(d) < 2 || d[0] != '0' {
		return false
	}
	switch d[1] {
	case 'x', 'X', 'b', 'B', 'o', 'O':
		return true
	default:
		return false
	}
}

// RadixLiteral splits an integer written in a non-decimal notation into its base and its
// digits, reporting false when s is not one.  A leading '-' is accepted and left for the
// caller to apply; the digits never carry it.
//
// The whole of s must be digits of that base, so "0x1f" splits and "0xzz" does not.  A
// prefix with nothing after it -- "0x" -- does not either.
//
// The tokenizer uses this to decide that a digit-leading run is a number after all, and
// parse uses it to read the value, so both work from one definition of the notation.
func RadixLiteral(s string) (base int, digits string, ok bool) {
	if strings.HasPrefix(s, "-") {
		s = s[1:]
	}
	if len(s) < 3 || s[0] != '0' {
		return 0, "", false
	}
	switch s[1] {
	case 'x', 'X':
		base = 16
	case 'o', 'O':
		base = 8
	case 'b', 'B':
		base = 2
	default:
		return 0, "", false
	}
	digits = s[2:]
	for i := 0; i < len(digits); i++ {
		if digitValue(digits[i]) >= base {
			return 0, "", false
		}
	}
	return base, digits, true
}

// RadixNotation names the notation of a radix literal's base, as one of the presentation
// tags a number can carry.  It returns "" for base 10, which has no tag.
func RadixNotation(base int) string {
	switch base {
	case 16:
		return "!hex"
	case 8:
		return "!oct"
	case 2:
		return "!bin"
	default:
		return ""
	}
}

// digitValue returns the value of c as a digit, or a number at least 16 when c is not one,
// so that a single comparison against the base rejects both a wrong-base digit and a
// non-digit.
func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 16
	}
}

func hasLetter(d []byte) bool {
	for _, r := range string(d) {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// dottedDigits reports whether d is three or more '.'-separated groups of ASCII digits,
// each non-empty: "1.2.3", "192.168.1.1".  Two groups is a float, which number() has
// already claimed, and an empty group is a mistyped number rather than a version.
func dottedDigits(d []byte) bool {
	groups := bytes.Split(d, []byte("."))
	if len(groups) < 3 {
		return false
	}
	for _, g := range groups {
		if len(g) == 0 {
			return false
		}
		for _, c := range g {
			if !asciiDigit(c) {
				return false
			}
		}
	}
	return true
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
