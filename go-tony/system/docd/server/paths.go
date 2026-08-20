package server

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// pathFields parses a kpath into its field-name segments (the object keys used to
// navigate the document for mount decomposition). It errors on a malformed path
// or any non-field segment (array/sparse index). A MOUNT path must be field-only --
// registration enforces it -- so this is the right question to ask about one.
//
// It is the wrong question to ask about a CLIENT path, which may address an array
// element: see fieldPrefix.
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

// fieldPrefix splits a client path into the leading field segments docd routes by, and
// whether anything else follows them -- an array or sparse index, which addresses INSIDE a
// value rather than naming a place in the document tree.
//
// Routing needs only the prefix, and that is not a convenience: a mount path is field-only,
// so no mount can be rooted at or below a non-field segment. A path holding one therefore
// cannot span mounts, has exactly one owner -- the deepest mount over its field prefix, or
// base -- and needs no decomposition at all.
//
// Asking pathFields instead refused the path outright, so EVERY write to an array element
// failed through docd, mounted or not: `a.votes[0]` was "a non-field segment", before any
// routing decision was made (yy0cfe9mh12kr6pwgsn0).
func fieldPrefix(p string) (fields []string, indexed bool, err error) {
	if p == "" {
		return nil, false, nil
	}
	if _, perr := kpath.Parse(p); perr != nil {
		return nil, false, fmt.Errorf("invalid path %q: %w", p, perr)
	}
	for _, seg := range kpath.SplitAll(p) {
		name, ok := kpath.SegmentFieldName(seg)
		if !ok {
			return fields, true, nil // an index: everything from here addresses inside a value
		}
		fields = append(fields, name)
	}
	return fields, false, nil
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
