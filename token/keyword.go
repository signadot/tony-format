package token

import (
	"unicode/utf8"
)

func isKeyWordPrefix(d, pre []byte) bool {
	if len(d) < len(pre) {
		return false
	}
	for i := range pre {
		if d[i] != pre[i] {
			return false
		}
	}
	if len(d) == len(pre) {
		return true
	}
	r, _ := utf8.DecodeRune(d[len(pre):])
	if r == ']' || r == '}' {
		// auto truncated literals...
		return true
	}
	if isMidLiteral(r) {
		return false
	}
	return true
}
