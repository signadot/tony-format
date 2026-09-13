package gomap

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// An embedded pointer to a struct promotes its fields as encoding/json does:
// through the pointer when set, nothing when nil, and allocated on decode when
// one of its fields arrives. It was dropped on both sides
// (4ynqp7wqh12krg32msn0 item 23).
func TestEmbeddedPointerStructPromotes(t *testing.T) {
	type Inner struct {
		A int
	}
	type Outer struct {
		*Inner
		X int
	}

	node, err := ToTonyIR(Outer{Inner: &Inner{A: 7}, X: 1})
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	m := ir.ToMap(node)
	if m["A"] == nil || m["A"].Int64 == nil || *m["A"].Int64 != 7 {
		t.Errorf("encoded without the embedded pointer's field: %v", m)
	}

	node, err = ToTonyIR(Outer{X: 1}) // nil Inner: nothing to promote
	if err != nil {
		t.Fatalf("ToTonyIR with nil embedded: %v", err)
	}
	if m := ir.ToMap(node); m["A"] != nil {
		t.Errorf("a nil embedded pointer contributed a field: %v", m)
	}

	var out Outer
	if err := FromTonyIR(ir.FromMap(map[string]*ir.Node{"A": ir.FromInt(7), "X": ir.FromInt(1)}), &out); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if out.Inner == nil || out.Inner.A != 7 || out.X != 1 {
		t.Errorf("decoded %+v (Inner %+v), want Inner allocated with A=7", out, out.Inner)
	}
	var untouched Outer
	if err := FromTonyIR(ir.FromMap(map[string]*ir.Node{"X": ir.FromInt(1)}), &untouched); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if untouched.Inner != nil {
		t.Errorf("an embedded pointer was allocated with none of its fields present: %+v", untouched.Inner)
	}
}
