package server

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// pathFields parses a kpath into its field-name segments (the object keys used to
// navigate the document for mount decomposition). It errors on a malformed path
// or any non-field segment (array/sparse index), which docd cannot use to route
// or split by mount. An empty path yields no segments.
//
// kpath.SplitAll panics on invalid input, so the path is validated with
// kpath.Parse first — callers may pass untrusted client paths.
func pathFields(p string) ([]string, error) {
	if p == "" {
		return nil, nil
	}
	if _, err := kpath.Parse(p); err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", p, err)
	}
	segs := kpath.SplitAll(p)
	fields := make([]string, len(segs))
	for i, seg := range segs {
		name, ok := kpath.SegmentFieldName(seg)
		if !ok {
			return nil, fmt.Errorf("path %q has a non-field segment %q", p, seg)
		}
		fields[i] = name
	}
	return fields, nil
}

// fieldsToKPath builds a kpath from field-name segments (with kpath's own
// quoting for fields that need it).
func fieldsToKPath(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	p := kpath.Field(fields[0]).String()
	for _, f := range fields[1:] {
		p = kpath.Join(p, kpath.Field(f).String())
	}
	return p
}

// hasFieldPrefix reports whether prefix is a segment-wise prefix of fields. An
// empty prefix (a root mount) matches everything.
func hasFieldPrefix(fields, prefix []string) bool {
	if len(fields) < len(prefix) {
		return false
	}
	for i := range prefix {
		if fields[i] != prefix[i] {
			return false
		}
	}
	return true
}

func fieldsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
