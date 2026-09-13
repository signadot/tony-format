package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/embedptr"
	"github.com/signadot/tony-format/go-tony/ir"
)

// Embedded structs (4ynqp7wqh12krg32msn0 item 27): a field promoted through an
// embedded POINTER is guarded -- nil contributes nothing on encode and is
// allocated on decode when a field of it arrives -- and a field the type declares
// itself shadows the promoted one, so the decoder's switch has one case for it.
func TestEmbeddedPointerAndShadowing(t *testing.T) {
	// (a) nil embedded pointer: neither side dereferences it.
	node, err := (&embedptr.S{Name: "n"}).ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR with nil Base: %v", err)
	}
	if m := ir.ToMap(node); m["id"] != nil || m["name"] == nil {
		t.Errorf("nil Base encoded as %v", m)
	}
	var s embedptr.S
	if err := s.FromTonyIR(ir.FromMap(map[string]*ir.Node{"name": ir.FromString("b")})); err != nil {
		t.Fatalf("FromTonyIR with no Base fields: %v", err)
	}
	if s.Base != nil || s.Name != "b" {
		t.Errorf("decoded %+v (Base %v), want Base left nil", s, s.Base)
	}
	if err := s.FromTonyIR(ir.FromMap(map[string]*ir.Node{"id": ir.FromString("i"), "name": ir.FromString("b")})); err != nil {
		t.Fatalf("FromTonyIR with a Base field: %v", err)
	}
	if s.Base == nil || s.Base.ID != "i" {
		t.Errorf("decoded Base %+v, want it allocated with ID=i", s.Base)
	}
	node, _ = (&embedptr.S{Base: &embedptr.Base{ID: "i"}, Name: "n"}).ToTonyIR()
	if m := ir.ToMap(node); m["id"] == nil || m["id"].String != "i" {
		t.Errorf("a set Base did not encode its field: %v", m)
	}

	// (b) the outer ID shadows Base's.
	tt := embedptr.T{ID: "outer"}
	tt.Base.ID = "inner"
	node, err = tt.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR T: %v", err)
	}
	if m := ir.ToMap(node); m["id"] == nil || m["id"].String != "outer" {
		t.Errorf("T encoded id %v, want the outer field's", m["id"])
	}
	var back embedptr.T
	if err := back.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR T: %v", err)
	}
	if back.ID != "outer" || back.Base.ID != "" {
		t.Errorf("T decoded to outer %q, inner %q; want outer set and inner untouched", back.ID, back.Base.ID)
	}
}
