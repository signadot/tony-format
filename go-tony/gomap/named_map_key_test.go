package gomap

import "testing"

// A map keyed by a named type decodes: SetMapIndex with a bare string or uint32
// panicked "value of type string is not assignable to type Key"
// (4ynqp7wqh12krg32msn0 item 22).
func TestNamedMapKeyTypeRoundTrips(t *testing.T) {
	type Key string
	type ID uint32
	type Doc struct {
		ByName map[Key]int
		ByID   map[ID]string
	}
	in := Doc{ByName: map[Key]int{"a": 1, "b": 2}, ByID: map[ID]string{3: "c", 7: "d"}}
	node, err := ToTonyIR(in)
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var out Doc
	if err := FromTonyIR(node, &out); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if out.ByName["a"] != 1 || out.ByName["b"] != 2 || out.ByID[3] != "c" || out.ByID[7] != "d" {
		t.Errorf("round-trip: %+v", out)
	}
}
