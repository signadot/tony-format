package codegen

import (
	"math"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/uints"
)

// An unsigned value above MaxInt64 has no number the format carries: the IR
// holds an int64, and the format rejects rather than rounds. The generated
// encoder wrote it as a negative number, which then could not be read back
// (p478tacqh12krg32msn0 item 23). It is refused, naming what held it.
func TestGeneratedRefusesAnUnsignedValueAboveInt64(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *uints.Host
		want string
	}{
		{"field", &uints.Host{U: math.MaxUint64}, `field "u"`},
		{"slice element", &uints.Host{U: 1, Xs: []uint64{1, math.MaxInt64 + 1}}, "element"},
		{"map value", &uints.Host{U: 1, M: map[string]uint64{"k": math.MaxUint64}}, "element"},
	} {
		_, err := tc.in.ToTonyIR()
		if err == nil || !strings.Contains(err.Error(), "does not fit int64") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want a refusal naming %s", tc.name, err, tc.want)
		}
	}

	in := &uints.Host{U: math.MaxInt64, N: math.MaxUint32, Xs: []uint64{7}, M: map[string]uint64{"k": 8}}
	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var out uints.Host
	if err := out.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if out.U != math.MaxInt64 || out.N != math.MaxUint32 || out.Xs[0] != 7 || out.M["k"] != 8 {
		t.Errorf("round trip: %+v", out)
	}
}
