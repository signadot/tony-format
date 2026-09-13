package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/floats"
)

// []float64 and map[string]float64 fields emitted ir.FromFloat64, which does not
// exist (4ynqp7wqh12krg32msn0 item 26).
func TestFloatCollectionsRoundTrip(t *testing.T) {
	in := &floats.Host{F: 1.5, Xs: []float64{0.5, 2}, M: map[string]float64{"k": 3.25}}
	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var out floats.Host
	if err := out.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if out.F != 1.5 || len(out.Xs) != 2 || out.Xs[1] != 2 || out.M["k"] != 3.25 {
		t.Errorf("round trip: %+v", out)
	}
}
