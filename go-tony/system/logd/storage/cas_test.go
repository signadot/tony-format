package storage

import (
	"fmt"
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TestStorage_CAS_ConcurrentSamePrecondition is a regression test for the CAS/PatchIf
// lost-update bug (issue r1w4k6g2): the match evaluation and the write were not serialized,
// so N concurrent conditional patches with the SAME precondition all evaluated against the
// same pre-commit state, all passed, and all committed — the "compare" of compare-and-swap
// was not atomic with the "swap".
//
// Contract under test: given N concurrent PatchIf that each require `gate == {v:0}` and set
// `gate` to a unique value, EXACTLY ONE must commit; the rest must observe the winner's write
// and fail their precondition.
func TestStorage_CAS_ConcurrentSamePrecondition(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	seed := func() {
		tx, err := s.NewTx(1, nil)
		if err != nil {
			t.Fatalf("seed NewTx: %v", err)
		}
		data, _ := parse.Parse([]byte(`{gate: {v: 0}}`))
		p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
		if err != nil {
			t.Fatalf("seed NewPatcher: %v", err)
		}
		if r := p.Commit(); !r.Committed {
			t.Fatalf("seed commit failed: %v", r.Error)
		}
	}

	// Several rounds widen the chance of catching the eval-before-write window.
	const rounds, racers = 6, 16
	for round := 0; round < rounds; round++ {
		seed() // gate.v = 0

		var wg sync.WaitGroup
		start := make(chan struct{})
		var mu sync.Mutex
		committed := 0

		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				tx, err := s.NewTx(1, nil)
				if err != nil {
					return
				}
				matchData, _ := parse.Parse([]byte(`{v: 0}`))                   // precondition: gate == {v:0}
				setData, _ := parse.Parse([]byte(fmt.Sprintf(`{v: %d}`, id+1))) // swap: gate = {v:id+1}
				p, err := tx.NewPatcher(&api.Patch{
					Match:    &api.PathData{Path: "gate", Data: matchData},
					PathData: api.PathData{Path: "gate", Data: setData},
				})
				if err != nil {
					return
				}
				<-start // release all racers together
				r := p.Commit()
				if r.Committed {
					mu.Lock()
					committed++
					mu.Unlock()
				}
			}(i)
		}
		close(start)
		wg.Wait()

		if committed != 1 {
			t.Fatalf("round %d: %d racers committed the same CAS precondition (want exactly 1) — lost update", round, committed)
		}
	}
}
