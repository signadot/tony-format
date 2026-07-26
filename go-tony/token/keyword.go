package token

import (
	"unicode/utf8"
)

// isKeyWordPrefix reports whether d holds the keyword pre and ends there rather
// than continuing into a longer literal. partial is true when the rune right
// after the keyword is cut off by the end of d, so the answer depends on bytes
// that have not been read yet and the caller must refill before deciding.
func isKeyWordPrefix(d, pre []byte) (ok, partial bool) {
	if len(d) < len(pre) {
		return false, false
	}
	for i := range pre {
		if d[i] != pre[i] {
			return false, false
		}
	}
	if len(d) == len(pre) {
		return true, false
	}
	rest := d[len(pre):]
	if partialRune(rest) {
		return false, true
	}
	r, _ := utf8.DecodeRune(rest)
	if r == ']' || r == '}' {
		// auto truncated literals...
		return true, false
	}
	if isMidLiteral(r) {
		return false, false
	}
	return true, false
}
