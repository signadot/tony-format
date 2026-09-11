package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

// A write of !nullify is stored as the value it leaves, not as the operation. It was
// stored as sent: !nullify rewrote the base it was folded onto, so the base and the next
// state were one object, their difference was nil, and the write kept an operation
// StorageContext excludes (fk1vg9sxh12ksyxxmdn0).
func TestNullifyIsStoredAsTheValueItLeaves(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{spec: {a: 1}, k: 1}`)
	c := mustCommit(t, s, nil, `{spec: !nullify null}`)

	cur, err := s.Deltas(c, c, nil, "")
	if err != nil {
		t.Fatalf("Deltas: %v", err)
	}
	defer cur.Close()
	n, err := cur.Next()
	if err != nil {
		t.Fatalf("the write's delta: %v", err)
	}
	if stored := encode.MustString(n.Patch); strings.Contains(stored, "!nullify") {
		t.Errorf("the log keeps the operation: %s", stored)
	}
	expectAt(t, s, nil, "", `{k: 1, spec: null}`)
}
