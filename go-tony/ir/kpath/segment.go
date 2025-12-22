package kpath

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/token"
)

type EntryKind int

const (
	FieldEntry EntryKind = iota
	ArrayEntry
	SparseArrayEntry
	KeyEntry
)

type SegmentType struct {
	EntryKind EntryKind
	Wild      bool
}

func (p *KPath) copySegment() *KPath {
	if p == nil {
		return nil
	}
	res := &KPath{}
	*res = *p
	if p.Field != nil {
		tmp := *p.Field
		res.Field = &tmp
		return res
	}
	if p.Index != nil {
		tmp := *p.Index
		res.Index = &tmp
		return res
	}
	if p.SparseIndex != nil {
		tmp := *p.SparseIndex
		res.SparseIndex = &tmp
		return res
	}
	if p.Key != nil {
		tmp := *p.Key
		res.Key = &tmp
		return res
	}
	return res
}

func segmentsEqual(a, b *KPath) bool {
	if (a.Field == nil) != (b.Field == nil) {
		return false
	}
	if a.Field != nil {
		return *a.Field == *b.Field
	}
	if a.FieldAll != b.FieldAll {
		return false
	}
	if (a.Index == nil) != (b.Index == nil) {
		return false
	}
	if a.Index != nil {
		return *a.Index == *b.Index
	}
	if a.IndexAll != b.IndexAll {
		return false
	}
	if (a.SparseIndex == nil) != (b.SparseIndex == nil) {
		return false
	}
	if a.SparseIndex != nil {
		return *a.SparseIndex == *b.SparseIndex
	}
	if a.SparseIndexAll != b.SparseIndexAll {
		return false
	}
	if (a.Key == nil) != (b.Key == nil) {
		return false
	}
	if a.Key != nil {
		return *a.Key == *b.Key
	}
	return true
}

func Field(f string) *KPath {
	return &KPath{
		Field: &f,
	}
}

func Index(i int) *KPath {
	return &KPath{
		Index: &i,
	}
}

func SparseIndex(i int) *KPath {
	return &KPath{
		SparseIndex: &i,
	}
}

func Key(t string) *KPath {
	return &KPath{
		Key: &t,
	}
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
	if p.Key != nil {
		return fmt.Sprintf("(%s)", *p.Key)
	}
	return ""
}
