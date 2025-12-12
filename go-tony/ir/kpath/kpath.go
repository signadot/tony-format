package kpath

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/signadot/tony-format/go-tony/token"
)

// KPath represents a kinded path (similar to Path but for kinded syntax).
// Kinded paths encode node kinds in the path syntax itself:
//   - "a.b" → Object accessed via ".b" (a is ObjectType)
//   - "a.*" → Object field wildcard (matches all fields)
//   - "a[0]" → Dense Array accessed via "[0]" (a is ArrayType)
//   - "a[*]" → Dense Array wildcard (matches all elements)
//   - "a{0}" → Sparse Array accessed via "{0}" (a is SparseArrayType)
//   - "a{*}" → Sparse Array wildcard (matches all sparse indices)
//
// Future: Support for !key(path) objects:
//   - "a.b(<value>)[2].fred" → Object with !key path value
type KPath struct {
	Field          *string // Object field name (e.g., "a", "b") - similar to Path.Field
	FieldAll       bool    // Object field wildcard .* - matches all fields
	Index          *int    // Dense array index (e.g., 0, 1) - similar to Path.Index
	IndexAll       bool    // Dense array wildcard [*] - similar to Path.IndexAll
	SparseIndex    *int    // Sparse array index (e.g., 0, 42) - for {n} syntax
	SparseIndexAll bool    // Sparse array wildcard {*} - matches all sparse indices
	Next           *KPath  // Next segment in path (nil for leaf) - similar to Path.Next
}

// String returns the kinded path string representation of this KPath.
// Example:
//
//	KPath{Field: &"a", Next: &KPath{Field: &"b", ...}} → "a.b"
//	KPath{Field: &"a", Next: &KPath{FieldAll: true, ...}} → "a.*"
//	KPath{Field: &"a", Next: &KPath{Index: &0, ...}} → "a[0]"
//	KPath{Field: &"a", Next: &KPath{IndexAll: true, ...}} → "a[*]"
//	KPath{Field: &"a", Next: &KPath{SparseIndex: &42, ...}} → "a{42}"
//	KPath{Field: &"a", Next: &KPath{SparseIndexAll: true, ...}} → "a{*}"
func (p *KPath) String() string {
	if p == nil {
		return ""
	}
	buf := bytes.NewBuffer(nil)
	x := p
	for x != nil {
		if x.FieldAll {
			// Field wildcard
			if buf.Len() > 0 {
				buf.WriteByte('.')
			}
			buf.WriteString("*")
			x = x.Next
			continue
		}
		if x.Field != nil {
			field := *x.Field
			// Check if we need a dot separator (not first segment)
			if buf.Len() > 0 {
				buf.WriteByte('.')
			}
			// Quote field if it contains spaces, dots, brackets, braces, or other special characters
			if token.KPathQuoteField(field) {
				buf.WriteString(token.Quote(field, true))
			} else {
				buf.WriteString(field)
			}
			x = x.Next
			continue
		}
		if x.IndexAll {
			buf.WriteString("[*]")
			x = x.Next
			continue
		}
		if x.Index != nil {
			fmt.Fprintf(buf, "[%d]", *x.Index)
			x = x.Next
			continue
		}
		if x.SparseIndexAll {
			buf.WriteString("{*}")
			x = x.Next
			continue
		}
		if x.SparseIndex != nil {
			fmt.Fprintf(buf, "{%d}", *x.SparseIndex)
			x = x.Next
			continue
		}
		x = x.Next
	}
	return buf.String()
}

// SegmentString returns the canonical string representation of this single segment.
// Unlike String(), this only returns the current segment, not the entire path.
// Examples:
//   - KPath{Field: &"a"} → "a"
//   - KPath{Field: &"field name"} → "'field name'" (quoted if needed)
//   - KPath{Index: &0} → "[0]"
//   - KPath{SparseIndex: &42} → "{42}"
//   - KPath{FieldAll: true} → "*"
//   - KPath{IndexAll: true} → "[*]"
//   - KPath{SparseIndexAll: true} → "{*}"
func (p *KPath) SegmentString() string {
	if p == nil {
		return ""
	}
	if p.FieldAll {
		return "*"
	}
	if p.Field != nil {
		field := *p.Field
		if token.KPathQuoteField(field) {
			return token.Quote(field, true)
		}
		return field
	}
	if p.IndexAll {
		return "[*]"
	}
	if p.Index != nil {
		return fmt.Sprintf("[%d]", *p.Index)
	}
	if p.SparseIndexAll {
		return "{*}"
	}
	if p.SparseIndex != nil {
		return fmt.Sprintf("{%d}", *p.SparseIndex)
	}
	return ""
}

// Parse parses a kinded path string into a KPath structure.
//
// Kinded path syntax:
//   - "a.b" → Object accessed via ".b"
//   - "a[0]" → Dense Array accessed via "[0]"
//   - "a[*]" → Dense Array wildcard (matches all elements)
//   - "a{0}" → Sparse Array accessed via "{0}"
//
// Examples:
//   - "a.b.c" → Object path with 3 segments
//   - "a[0][1]" → Dense array path with 3 segments
//   - "a[*].b" → Array wildcard then object
//   - "a{0}.b" → Sparse array then object
//   - "" → Root path (returns nil)
//
// Returns an error if the path syntax is invalid.
func Parse(kpath string) (*KPath, error) {
	if kpath == "" {
		return nil, nil
	}
	root := &KPath{}
	err := parseKFrag(kpath, root)
	if err != nil {
		return nil, err
	}
	return root, nil
}

// Split splits a kinded path into the first segment and the remaining path.
// Returns the first segment as a string (suitable for use as a map key) and the rest of the path.
// Panics if the path cannot be parsed (invalid kinded path syntax).
//
// Examples:
//   - Split("a.b.c") → ("a", "b.c")
//   - Split("[0].b") → ("[0]", "b")
//   - Split("{13}.c") → ("{13}", "c")
//   - Split("a") → ("a", "")
//   - Split("") → ("", "")
//
// The first segment is returned as a string representation:
//   - Field: "a" or "'field name'" (quoted if needed)
//   - Dense array: "[0]"
//   - Sparse array: "{0}"
func Split(kpath string) (firstSegment string, restPath string) {
	if kpath == "" {
		return "", ""
	}

	kp, err := Parse(kpath)
	if err != nil {
		panic(fmt.Sprintf("Split: invalid kinded path %q: %v", kpath, err))
	}
	if kp == nil {
		return "", ""
	}

	// Extract first segment as string
	firstSegment = segmentToString(kp)

	// Reconstruct rest of path
	if kp.Next == nil {
		restPath = ""
	} else {
		restPath = kp.Next.String()
	}

	return firstSegment, restPath
}

// RSplit splits a kinded path into the parent path and the last segment.
// Returns the parent path (everything except the last segment) and the last segment.
// Panics if the path cannot be parsed (invalid kinded path syntax).
//
// Examples:
//   - RSplit("a.b.c") → ("a.b", "c")
//   - RSplit("a[0]") → ("a", "[0]")
//   - RSplit("[0].b") → ("[0]", "b")
//   - RSplit("{13}.c") → ("{13}", "c")
//   - RSplit("a") → ("", "a")
//   - RSplit("") → ("", "")
//
// The last segment is returned as a string representation:
//   - Field: "a" or "'field name'" (quoted if needed)
//   - Dense array: "[0]"
//   - Sparse array: "{0}"
func RSplit(kpath string) (parentPath string, lastSegment string) {
	if kpath == "" {
		return "", ""
	}

	kp, err := Parse(kpath)
	if err != nil {
		panic(fmt.Sprintf("RSplit: invalid kinded path %q: %v", kpath, err))
	}
	if kp == nil {
		return "", ""
	}

	// Find the last segment by traversing to the end
	// We need to find the second-to-last node to build the parent path
	var prev *KPath
	current := kp
	for current.Next != nil {
		prev = current
		current = current.Next
	}

	// Extract last segment
	lastSegment = current.SegmentString()

	// Reconstruct parent path
	if prev == nil {
		// Only one segment, parent is empty
		parentPath = ""
	} else {
		// Build parent by creating a new KPath chain ending at prev
		parentKP := &KPath{}
		copyKPathSegment(kp, parentKP, prev.Next)
		parentPath = parentKP.String()
	}

	return parentPath, lastSegment
}

// SplitAll splits a kinded path into all segments from root to leaf.
// Returns a slice of segment strings, each representing a valid top-level kpath.
// Panics if the path cannot be parsed (invalid kinded path syntax).
//
// Examples:
//   - SplitAll("a.b.c") → ["a", "b", "c"]
//   - SplitAll("[0].b") → ["[0]", "b"]
//   - SplitAll("{13}.c") → ["{13}", "c"]
//   - SplitAll("a") → ["a"]
//   - SplitAll("") → []
//
// Each segment is a valid top-level kpath that will parse:
//   - Field: "a" or "'field name'" (quoted if needed)
//   - Dense array: "[0]" or "[*]"
//   - Sparse array: "{0}" or "{*}"
//   - Field wildcard: "*" (top-level) or ".*" (nested)
func SplitAll(kpath string) []string {
	if kpath == "" {
		return []string{}
	}

	kp, err := Parse(kpath)
	if err != nil {
		panic(fmt.Sprintf("SplitAll: invalid kinded path %q: %v", kpath, err))
	}
	if kp == nil {
		return []string{}
	}

	var segments []string
	current := kp
	for current != nil {
		segments = append(segments, segmentToString(current))
		current = current.Next
	}

	return segments
}

// SegmentFieldName extracts the field name from a segment string.
// Returns the unquoted field name and true if the segment is a field segment.
// Returns false if the segment is not a field (e.g., array index "[0]", sparse index "{0}", wildcard "*").
//
// Examples:
//   - SegmentFieldName("a") → ("a", true)
//   - SegmentFieldName("'field name'") → ("field name", true)
//   - SegmentFieldName("\"field name\"") → ("field name", true)
//   - SegmentFieldName("[0]") → ("", false)
//   - SegmentFieldName("{0}") → ("", false)
//   - SegmentFieldName("*") → ("", false)
func SegmentFieldName(segment string) (fieldName string, isField bool) {
	if segment == "" {
		return "", false
	}

	// Parse the segment as a kpath
	kp, err := Parse(segment)
	if err != nil {
		// Invalid segment - not a field
		return "", false
	}
	if kp == nil {
		return "", false
	}

	// Check if it's a field segment
	if kp.Field != nil {
		return *kp.Field, true
	}

	// Not a field (could be array index, sparse index, wildcard, etc.)
	return "", false
}

// copyKPathSegment copies KPath segments from src to dst up to (but not including) stop.
// If stop is nil, copies the entire chain.
func copyKPathSegment(src *KPath, dst *KPath, stop *KPath) {
	if src == stop {
		return
	}
	// Copy current segment
	if src.FieldAll {
		dst.FieldAll = true
	} else if src.Field != nil {
		field := *src.Field
		dst.Field = &field
	} else if src.IndexAll {
		dst.IndexAll = true
	} else if src.Index != nil {
		idx := *src.Index
		dst.Index = &idx
	} else if src.SparseIndexAll {
		dst.SparseIndexAll = true
	} else if src.SparseIndex != nil {
		idx := *src.SparseIndex
		dst.SparseIndex = &idx
	}
	// Copy next segment if not at stop
	if src.Next != nil && src.Next != stop {
		dst.Next = &KPath{}
		copyKPathSegment(src.Next, dst.Next, stop)
	}
}

// segmentToString converts the first segment of a KPath to its string representation.
// Each segment is treated as a top-level kpath, so FieldAll outputs "*" not ".*".
func segmentToString(kp *KPath) string {
	if kp.FieldAll {
		return "*"
	}
	if kp.Field != nil {
		field := *kp.Field
		if token.KPathQuoteField(field) {
			return token.Quote(field, true)
		}
		return field
	}
	if kp.IndexAll {
		return "[*]"
	}
	if kp.Index != nil {
		return fmt.Sprintf("[%d]", *kp.Index)
	}
	if kp.SparseIndexAll {
		return "{*}"
	}
	if kp.SparseIndex != nil {
		return fmt.Sprintf("{%d}", *kp.SparseIndex)
	}
	return ""
}

// Join joins a prefix segment with a suffix kinded path.
// The prefix should be a single segment (field name, [index], or {index}).
// Returns the combined kinded path string.
//
// Examples:
//   - Join("a", "b.c") → "a.b.c"
//   - Join("a", "[0]") → "a[0]"
//   - Join("[0]", "b") → "[0].b"
//   - Join("a", "") → "a"
//   - Join("", "b") → "b"
func Join(prefix string, suffix string) string {
	if prefix == "" {
		return suffix
	}
	if suffix == "" {
		return prefix
	}

	// Parse suffix to get the KPath structure
	suffixKp, err := Parse(suffix)
	if err != nil {
		// If suffix doesn't parse, just concatenate (fallback)
		// This handles edge cases where suffix might be malformed
		return prefix + suffix
	}

	// Parse prefix as a single segment
	prefixKp, err := parseSingleSegment(prefix)
	if err != nil {
		// If prefix doesn't parse as a segment, just concatenate (fallback)
		return prefix + suffix
	}

	// Link prefix segment to suffix path
	if suffixKp == nil {
		// Suffix is empty, just return prefix
		return prefix
	}

	// Find the end of prefix and attach suffix
	last := prefixKp
	for last.Next != nil {
		last = last.Next
	}
	last.Next = suffixKp

	return prefixKp.String()
}

// parseSingleSegment parses a single segment string into a KPath.
// Handles: field names (quoted or unquoted), [index], {index}, [*], {*}, .*
func parseSingleSegment(seg string) (*KPath, error) {
	if seg == "" {
		return nil, fmt.Errorf("empty segment")
	}

	kp := &KPath{}

	// Check for wildcards first
	if seg == ".*" {
		kp.FieldAll = true
		return kp, nil
	}
	if seg == "[*]" {
		kp.IndexAll = true
		return kp, nil
	}
	if seg == "{*}" {
		kp.SparseIndexAll = true
		return kp, nil
	}

	// Check for array/sparse array indices
	if len(seg) > 0 && seg[0] == '[' {
		if seg[len(seg)-1] != ']' {
			return nil, fmt.Errorf("unclosed bracket in segment %q", seg)
		}
		indexStr := seg[1 : len(seg)-1]
		if indexStr == "*" {
			kp.IndexAll = true
		} else {
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return nil, fmt.Errorf("invalid array index %q: %w", indexStr, err)
			}
			kp.Index = &index
		}
		return kp, nil
	}

	if len(seg) > 0 && seg[0] == '{' {
		if seg[len(seg)-1] != '}' {
			return nil, fmt.Errorf("unclosed brace in segment %q", seg)
		}
		indexStr := seg[1 : len(seg)-1]
		if indexStr == "*" {
			kp.SparseIndexAll = true
		} else {
			index, err := strconv.Atoi(indexStr)
			if err != nil {
				return nil, fmt.Errorf("invalid sparse index %q: %w", indexStr, err)
			}
			kp.SparseIndex = &index
		}
		return kp, nil
	}

	// Must be a field name (possibly quoted)
	field := seg
	if (seg[0] == '"' || seg[0] == '\'') && seg[len(seg)-1] == seg[0] {
		// Quoted field - unquote it
		field = token.QuotedToString([]byte(seg))
	}
	kp.Field = &field
	return kp, nil
}

// parseKFrag parses a fragment of a kinded path string.
func parseKFrag(frag string, parent *KPath) error {
	if len(frag) == 0 {
		return nil
	}
	switch frag[0] {
	case '.':
		// Check for wildcard .*
		if len(frag) > 1 && frag[1] == '*' {
			parent.FieldAll = true
			if len(frag) == 2 {
				return nil
			}
			next := &KPath{}
			err := parseKFrag(frag[2:], next)
			if err != nil {
				return err
			}
			parent.Next = next
			return nil
		}
		field, rest, err := parseKField(frag[1:])
		if err != nil {
			return err
		}
		parent.Field = &field
		if len(rest) == 0 {
			return nil
		}
		next := &KPath{}
		err = parseKFrag(rest, next)
		if err != nil {
			return err
		}
		parent.Next = next
		return nil
	case '[':
		i := strings.IndexByte(frag[1:], ']')
		if i == -1 {
			return fmt.Errorf("expected '[' <index> ']'")
		}
		index, all, err := parseKIndex(frag[1 : i+1])
		if err != nil {
			return err
		}
		parent.IndexAll = all
		if !all {
			parent.Index = &index
		}
		if len(frag) == i+2 {
			return nil
		}
		next := &KPath{}
		err = parseKFrag(frag[i+2:], next)
		if err != nil {
			return err
		}
		parent.Next = next
		return nil
	case '{':
		i := strings.IndexByte(frag[1:], '}')
		if i == -1 {
			return fmt.Errorf("expected '{' <index> '}'")
		}
		index, all, err := parseKSparseIndex(frag[1 : i+1])
		if err != nil {
			return err
		}
		parent.SparseIndexAll = all
		if !all {
			parent.SparseIndex = &index
		}
		if len(frag) == i+2 {
			return nil
		}
		next := &KPath{}
		err = parseKFrag(frag[i+2:], next)
		if err != nil {
			return err
		}
		parent.Next = next
		return nil
	case '*':
		// Top-level wildcard for all fields
		// Check if it's followed by a separator or is the entire fragment
		if len(frag) == 1 {
			// Just "*" - all fields at top level
			parent.FieldAll = true
			return nil
		}
		// Check what comes after *
		if len(frag) > 1 {
			nextChar := frag[1]
			if nextChar == '.' || nextChar == '[' || nextChar == '{' {
				// "*." or "*[" or "*{" - all fields followed by more path
				parent.FieldAll = true
				next := &KPath{}
				err := parseKFrag(frag[1:], next)
				if err != nil {
					return err
				}
				parent.Next = next
				return nil
			}
		}
		// Fall through to parse as literal field name "*"
		fallthrough
	default:
		// Start with a field (no leading dot)
		field, rest, err := parseKField(frag)
		if err != nil {
			return fmt.Errorf("expected field, '[', or '{', got %q", frag[0])
		}
		parent.Field = &field
		if len(rest) == 0 {
			return nil
		}
		next := &KPath{}
		err = parseKFrag(rest, next)
		if err != nil {
			return err
		}
		parent.Next = next
		return nil
	}
}

// parseKIndex parses a dense array index from a string like "0", "42", or "*".
// Returns (index, all, error) where all is true if the index is "*".
func parseKIndex(is string) (index int, all bool, err error) {
	if len(is) == 1 && is[0] == '*' {
		return 0, true, nil
	}
	u64, err := strconv.ParseUint(is, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid array index %q: %v", is, err)
	}
	return int(u64), false, nil
}

// parseKSparseIndex parses a sparse array index from a string like "0", "42", or "*".
// Returns (index, all, error) where all is true if the index is "*".
func parseKSparseIndex(is string) (index int, all bool, err error) {
	if len(is) == 1 && is[0] == '*' {
		return 0, true, nil
	}
	u64, err := strconv.ParseUint(is, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("invalid sparse array index %q: %v", is, err)
	}
	return int(u64), false, nil
}

// parseKField parses an object field name from a fragment.
// It stops at '.', '[', or '{' characters.
// Supports tony quoted strings (single or double quotes with escape sequences).
func parseKField(frag string) (field, rest string, err error) {
	if len(frag) == 0 {
		return "", "", fmt.Errorf("expected field at end of string")
	}
	// Check if field starts with a quote character
	if frag[0] == '\'' || frag[0] == '"' {
		// Parse quoted string using token package logic
		quotedLen, err := findQuotedStringEnd([]byte(frag))
		if err != nil {
			return "", "", fmt.Errorf("invalid quoted field: %w", err)
		}
		quotedPortion := frag[:quotedLen]
		// Unquote using token.QuotedToString (which handles escapes)
		field = token.QuotedToString([]byte(quotedPortion))
		rest = frag[quotedLen:]
		return field, rest, nil
	}
	// Unquoted field: find the first occurrence of '.', '[', or '{'
	i := strings.IndexAny(frag, ".[{")
	if i == -1 {
		return frag, "", nil
	}
	return frag[:i], frag[i:], nil
}

// findQuotedStringEnd finds the end of a quoted string in a byte slice.
// Returns the length consumed (including the closing quote).
// Uses the same logic as token.bsEscQuoted but doesn't require the full string to be quoted.
func findQuotedStringEnd(d []byte) (int, error) {
	if len(d) == 0 {
		return 0, fmt.Errorf("empty string")
	}
	quoteChar := rune(d[0])
	if quoteChar != '\'' && quoteChar != '"' {
		return 0, fmt.Errorf("not a quoted string")
	}
	escaped := false
	start := 1
	n := len(d)
	for start < n {
		r, sz := utf8.DecodeRune(d[start:])
		if r == utf8.RuneError {
			return 0, token.ErrBadUTF8
		}
		start += sz
		switch r {
		case quoteChar:
			if !escaped {
				return start, nil
			}
			escaped = false
		case 'u':
			if escaped {
				if start+4 > n {
					return 0, token.ErrUnterminated
				}
				// Check if next 4 bytes are hex
				allHex := true
				for i := start; i < start+4 && i < n; i++ {
					c := d[i]
					if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
						allHex = false
						break
					}
				}
				if !allHex {
					return start, token.ErrBadUnicode
				}
				start += 4
			}
			escaped = false
		case '/', 'b', 'f', 'n', 'r', 't':
			escaped = false
		case '\\':
			escaped = !escaped
		default:
			if escaped {
				return start, token.ErrBadEscape
			}
			escaped = false
		}
	}
	return 0, token.ErrUnterminated
}

// Parent returns the parent path (all segments except the last).
// Returns nil if this is already the root segment or if there's only one segment.
func (p *KPath) Parent() *KPath {
	if p == nil || p.Next == nil {
		return nil
	}
	// Count segments
	count := 0
	for x := p; x != nil; x = x.Next {
		count++
	}
	if count <= 1 {
		return nil
	}
	// Build parent path (all but last segment)
	parent := &KPath{}
	current := parent
	x := p
	for i := 0; i < count-1; i++ {
		if x.FieldAll {
			current.FieldAll = true
		}
		if x.Field != nil {
			f := *x.Field
			current.Field = &f
		}
		if x.IndexAll {
			current.IndexAll = true
		}
		if x.Index != nil {
			idx := *x.Index
			current.Index = &idx
		}
		if x.SparseIndexAll {
			current.SparseIndexAll = true
		}
		if x.SparseIndex != nil {
			idx := *x.SparseIndex
			current.SparseIndex = &idx
		}
		x = x.Next
		if i < count-2 {
			current.Next = &KPath{}
			current = current.Next
		}
	}
	return parent
}

// IsChildOf returns true if this path is a child of the given parent path.
func (p *KPath) IsChildOf(parent *KPath) bool {
	if parent == nil {
		return p != nil
	}
	if p == nil {
		return false
	}
	// Check if p starts with parent
	pp := p
	pparent := parent
	for pparent != nil {
		if pp == nil {
			return false
		}
		// Compare segments
		if !kpathSegmentsEqual(pp, pparent) {
			return false
		}
		pp = pp.Next
		pparent = pparent.Next
	}
	return true
}

// kpathSegmentsEqual compares two KPath segments for equality.
func kpathSegmentsEqual(a, b *KPath) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.FieldAll != b.FieldAll {
		return false
	}
	if a.Field != nil && b.Field != nil {
		if *a.Field != *b.Field {
			return false
		}
	} else if a.Field != nil || b.Field != nil {
		return false
	}
	if a.IndexAll != b.IndexAll {
		return false
	}
	if a.Index != nil && b.Index != nil {
		if *a.Index != *b.Index {
			return false
		}
	} else if a.Index != nil || b.Index != nil {
		return false
	}
	if a.SparseIndexAll != b.SparseIndexAll {
		return false
	}
	if a.SparseIndex != nil && b.SparseIndex != nil {
		if *a.SparseIndex != *b.SparseIndex {
			return false
		}
	} else if a.SparseIndex != nil || b.SparseIndex != nil {
		return false
	}
	return true
}

// Compare compares two paths lexicographically.
// Returns -1 if p < other, 0 if p == other, 1 if p > other.
func (p *KPath) Compare(other *KPath) int {
	if p == nil && other == nil {
		return 0
	}
	if p == nil {
		return -1
	}
	if other == nil {
		return 1
	}
	// Compare segments one by one
	pa := p
	pb := other
	for pa != nil && pb != nil {
		cmp := compareKPathSegment(pa, pb)
		if cmp != 0 {
			return cmp
		}
		pa = pa.Next
		pb = pb.Next
	}
	if pa == nil && pb == nil {
		return 0
	}
	if pa == nil {
		return -1
	}
	return 1
}

// compareKPathSegment compares two KPath segments.
func compareKPathSegment(a, b *KPath) int {
	// Compare by type: Field < FieldAll < Index < IndexAll < SparseIndex < SparseIndexAll
	if a.Field != nil && b.Field != nil {
		if *a.Field < *b.Field {
			return -1
		}
		if *a.Field > *b.Field {
			return 1
		}
		return 0
	}
	if a.Field != nil {
		return -1
	}
	if b.Field != nil {
		return 1
	}
	if a.FieldAll && b.FieldAll {
		return 0
	}
	if a.FieldAll {
		return 1
	}
	if b.FieldAll {
		return -1
	}
	if a.Index != nil && b.Index != nil {
		if *a.Index < *b.Index {
			return -1
		}
		if *a.Index > *b.Index {
			return 1
		}
		return 0
	}
	if a.Index != nil {
		return -1
	}
	if b.Index != nil {
		return 1
	}
	if a.IndexAll && b.IndexAll {
		return 0
	}
	if a.IndexAll {
		return 1
	}
	if b.IndexAll {
		return -1
	}
	if a.SparseIndex != nil && b.SparseIndex != nil {
		if *a.SparseIndex < *b.SparseIndex {
			return -1
		}
		if *a.SparseIndex > *b.SparseIndex {
			return 1
		}
		return 0
	}
	if a.SparseIndex != nil {
		return -1
	}
	if b.SparseIndex != nil {
		return 1
	}
	if a.SparseIndexAll && b.SparseIndexAll {
		return 0
	}
	if a.SparseIndexAll {
		return 1
	}
	if b.SparseIndexAll {
		return -1
	}
	return 0
}

func (kp *KPath) MarshalText() ([]byte, error) {
	return []byte(kp.String()), nil
}

func (kp *KPath) UnmarshalText(d []byte) error {
	pp, err := Parse(string(d))
	if err != nil {
		return err
	}
	*kp = *pp
	return nil
}
