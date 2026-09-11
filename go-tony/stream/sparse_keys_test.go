package stream

import (
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// A sparse array read back from events has the keys parse gives it: integers, each with
// its decimal in String, and each value knowing its key as ParentField. EventsToNode left
// both empty, and a merge names a field by its String, so a sparse array folded from
// events collapsed onto one "" key (0bns6k1wh12ksyxxmdn0).
func TestASparseArrayReadFromEventsKeepsItsKeys(t *testing.T) {
	sp, err := parse.Parse([]byte(`!sparsearray {0: y, 7: z}`))
	if err != nil {
		t.Fatal(err)
	}
	evs, err := NodeToEvents(sp)
	if err != nil {
		t.Fatal(err)
	}
	back, err := EventsToNode(evs)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Fields) != 2 {
		t.Fatalf("read back %d keys, want 2", len(back.Fields))
	}
	for i, f := range back.Fields {
		want := sp.Fields[i]
		if f.Int64 == nil || *f.Int64 != *want.Int64 || f.String != strconv.FormatInt(*want.Int64, 10) {
			t.Errorf("key %d is %v %q, want %d", i, f.Int64, f.String, *want.Int64)
		}
		if got := back.Values[i].ParentField; got != f.String {
			t.Errorf("value %d says it is field %q, held as %q", i, got, f.String)
		}
	}
}
