package codegen

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedint"
)

// A field of a named local integer type (`type Level int`) decoded to int, not
// Level, and the generated code did not compile; the fixture building is most of
// the test (p478tacqh12krg32msn0 item 25).
func TestNamedIntFieldRoundTrips(t *testing.T) {
	p := namedint.Level(3)
	in := &namedint.Host{L: 2, P: &p, D: time.Second}
	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var out namedint.Host
	if err := out.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if out.L != 2 || out.P == nil || *out.P != 3 || out.D != time.Second {
		t.Errorf("round trip: %+v", out)
	}
}
