package gomap

import (
	"errors"
	"math"
	"strings"
	"testing"
)

// The reflection encoder wrote a uint64 above MaxInt64 as a negative number,
// which then could not be read back (p478tacqh12krg32msn0 item 23). It is
// refused, naming the field.
func TestReflectionRefusesAnUnsignedValueAboveInt64(t *testing.T) {
	type host struct {
		U  uint64   `tony:"field=u"`
		Xs []uint64 `tony:"field=xs"`
	}
	_, err := ToTonyIR(&host{U: math.MaxUint64})
	var me *MarshalError
	if !errors.As(err, &me) || me.FieldPath != "u" || !strings.Contains(me.Message, "does not fit int64") {
		t.Errorf("field: err %v, want a MarshalError at u saying it does not fit int64", err)
	}
	if _, err := ToTonyIR(&host{Xs: []uint64{math.MaxInt64 + 1}}); err == nil {
		t.Error("slice element above MaxInt64 was written")
	}
	node, err := ToTonyIR(&host{U: math.MaxInt64, Xs: []uint64{1}})
	if err != nil {
		t.Fatalf("in range: %v", err)
	}
	var out host
	if err := FromTonyIR(node, &out); err != nil || out.U != math.MaxInt64 || out.Xs[0] != 1 {
		t.Errorf("round trip: %+v, %v", out, err)
	}
}
