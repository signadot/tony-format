package eval

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// !toint answers the integer a float names, and refuses a float which names none.
// Every float failed with strconv's `parsing ""`: the float branch set the int and
// then fell through to parse the Number string, which a float leaves empty
// (addsgv1yh12kszdxmdn0).
func TestToIntOfAFloat(t *testing.T) {
	op, err := ToInt().Instance(nil, nil)
	if err != nil {
		t.Fatalf("Instance: %v", err)
	}

	got, err := op.Eval(ir.FromFloat(3), Env{}, nil)
	if err != nil {
		t.Fatalf("3.0: %v", err)
	}
	if got.Type != ir.NumberType || got.Int64 == nil || *got.Int64 != 3 || got.Float64 != nil {
		t.Errorf("3.0 gave %+v, want the int 3", got)
	}

	for _, tc := range []struct {
		f    float64
		text string
	}{
		{3.5, "3.5"},    // not a whole number
		{1e30, "1e+30"}, // a whole number no int64 holds
	} {
		_, err := op.Eval(ir.FromFloat(tc.f), Env{}, nil)
		if err == nil {
			t.Errorf("%s converted to an int", tc.text)
			continue
		}
		if !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: the error does not name the number: %v", tc.text, err)
		}
	}
}
