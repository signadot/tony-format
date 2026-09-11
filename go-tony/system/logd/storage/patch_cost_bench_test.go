package storage

import (
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// benchSet is a store holding a set of n entities under verse.entities.
func benchSet(b *testing.B, n int) *Storage {
	b.Helper()
	s, err := Open(b.TempDir(), nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	set := make(map[string]*ir.Node, n)
	for i := 0; i < n; i++ {
		id := "e" + strconv.Itoa(i)
		set[id] = ir.FromMap(map[string]*ir.Node{"id": ir.FromString(id), "status": ir.FromString("new")})
	}
	benchCommit(b, s, "verse.entities", ir.FromMap(set))
	return s
}

func benchCommit(b *testing.B, s *Storage, path string, data *ir.Node) {
	b.Helper()
	tx, err := s.NewTx(1, nil)
	if err != nil {
		b.Fatal(err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		b.Fatal(err)
	}
	if res := p.Commit(); !res.Committed {
		b.Fatalf("commit at %s: %v", path, res.Error)
	}
}

func statusWrite(status string) *ir.Node {
	return ir.FromMap(map[string]*ir.Node{"status": ir.FromString(status)})
}

// BenchmarkCommitIntoSet is the write path for one field of one entity in a set of n:
// lowering, the merge, the log and the index.
func BenchmarkCommitIntoSet(b *testing.B) {
	for _, n := range []int{200, 3000} {
		b.Run(fmt.Sprintf("entities=%d", n), func(b *testing.B) {
			s := benchSet(b, n)
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				benchCommit(b, s, "verse.entities.e"+strconv.Itoa(i%n), statusWrite("s"+strconv.Itoa(i)))
				i++
			}
		})
	}
}

// BenchmarkReadSetAfterWrites reads the whole set after 64 writes into it: the fold of the
// records since the snapshot at the set's path.
func BenchmarkReadSetAfterWrites(b *testing.B) {
	for _, n := range []int{200, 3000} {
		b.Run(fmt.Sprintf("entities=%d/writes=64", n), func(b *testing.B) {
			s := benchSet(b, n)
			for i := 0; i < 64; i++ {
				benchCommit(b, s, "verse.entities.e"+strconv.Itoa(i*(n/64)), statusWrite("s"+strconv.Itoa(i)))
			}
			head, err := s.GetCurrentCommit()
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				cur, err := s.Read(head, nil, "verse.entities")
				if err != nil {
					b.Fatal(err)
				}
				if _, err := Collect(cur, math.MaxInt64); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
