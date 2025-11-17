package schema

import "strings"

func EscapeRef(r string) string {
	for i, c := range r {
		switch c {
		case '.':
			return strings.Replace(r[:i], "\\", "\\\\", -1) + "\\" + r[i:]
		case '\\':
		default:
			return r
		}
	}
	return r
}

func UnescapeRef(e string) string {
	for i, c := range e {
		switch c {
		case '.':
			return strings.Replace(e[:i], "\\\\", "\\", -1) + e[i:]
		case '\\':
		default:
			return e
		}
	}
	return e
}
