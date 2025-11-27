package storage

import (
	"fmt"
	"strconv"
)

func FormatLexInt(v int64) string {
	d := strconv.FormatUint(uint64(v), 10)
	prefix := rune('a' + len(d) - 1)
	return string(prefix) + d
}

func ParseLexInt(v string) (int64, error) {
	if len(v) < 2 {
		return 0, fmt.Errorf("%q too short", v)
	}
	if 'a' <= v[0] && v[0] <= 's' {
		u, err := strconv.ParseUint(v[1:], 10, 64)
		if err != nil {
			return 0, err
		}
		return int64(u), nil

	}
	return 0, fmt.Errorf("invalid leading character in %q, expecting a-s", v)
}
