package storage

import "testing"

// A sparse array is read back as it was written. Its integer keys came back as "", so
// every entry collapsed onto one key and the last one won: {0: y, 1: z} read as
// {"": z} (0bns6k1wh12ksyxxmdn0). A later write merges into it by key.
func TestASparseArrayReadsBackAsWritten(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{x: !sparsearray {0: y, 1: z}}`)
	expectAt(t, s, nil, "x", `!sparsearray {0: y, 1: z}`)
	mustCommit(t, s, nil, `{x: !sparsearray {1: w, 5: v}}`)
	expectAt(t, s, nil, "x", `!sparsearray {0: y, 1: w, 5: v}`)
}
