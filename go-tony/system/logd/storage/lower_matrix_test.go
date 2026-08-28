package storage

import (
	"math/rand"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// applyOpLoweredByClient commits the op as a CLIENT that lowers for itself: it reads
// the state, applies the op locally, diffs, and sends the diff as an ordinary patch at
// the root. The engine's own lowering stays off, so the write takes the same path any
// client write takes.
//
// It is the row that separates the two questions the engine-side lowering asks at
// once. If a divergence follows the delta here, it is the delta's SHAPE and the engine
// is innocent; if it does not, the shape is fine and what matters is where the engine
// computes it -- inside the commit, against a base of its own choosing.
func applyOpLoweredByClient(t *testing.T, s *Storage, o genOp) (int64, error) {
	t.Helper()
	n, err := parse.Parse([]byte(o.src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", o.src, err)
	}
	commit, err := s.GetCurrentCommit()
	if err != nil {
		return 0, err
	}
	base, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		return 0, err
	}
	if base == nil {
		base = ir.Null()
	}
	// The client's own patch, rooted where it means to write, so the local apply is
	// the same one the engine would do.
	rooted := n
	if o.path != "" {
		rooted, err = tx.RootPatchAt(o.path, n)
		if err != nil {
			return 0, err
		}
	}
	next, err := api.NextState(base.Clone(), rooted.Clone())
	if err != nil {
		return 0, err
	}
	if next == nil {
		next = ir.Null()
	}
	delta := tony.DiffWith(base.Clone(), next.Clone(),
		tony.DiffComments(true), tony.DiffAbsolute(true))
	if delta == nil {
		// Nothing changed. A patch is still sent, so the commit stream matches the
		// other rows commit for commit.
		delta = ir.FromMap(map[string]*ir.Node{})
	}
	txn, err := s.NewTx(1, nil)
	if err != nil {
		return 0, err
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: delta}})
	if err != nil {
		return 0, err
	}
	res := p.Commit()
	if !res.Committed {
		return 0, res.Error
	}
	return res.Commit, nil
}

// The matrix: the same generated stream, committed three ways, each checked against
// its own head at every commit.
//
//	plain    the client's patch, stored as sent
//	engine   the engine lowers inside the commit (LowerEverything)
//	client   the client lowers and sends the delta; the engine stores it as sent
//
// The third is the one that separates delta SHAPE from where the engine computes it.
func TestLoweringMatrix(t *testing.T) {
	rows := []struct {
		name  string
		open  func(t *testing.T) *Storage
		apply func(t *testing.T, s *Storage, o genOp) (int64, error)
	}{{
		name: "plain",
		open: func(t *testing.T) *Storage {
			s := openTestStorage(t)
			s.EnableLowering(false) // explicitly, since lowering is now the default
			return s
		},
		apply: applyOp,
	}, {
		name: "engine",
		open: func(t *testing.T) *Storage {
			s := openTestStorage(t)
			s.LowerEverything(true)
			return s
		},
		apply: applyOp,
	}, {
		name: "client",
		open: func(t *testing.T) *Storage {
			s := openTestStorage(t)
			// The CLIENT lowers; the engine must not, or the delta is lowered
			// twice and the row stops answering what it is for.
			s.EnableLowering(false)
			return s
		},
		apply: applyOpLoweredByClient,
	}}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			dropped, diverged := 0, 0
			for seed := 1; seed <= seedCount(); seed++ {
				rng := rand.New(rand.NewSource(int64(seed)))
				ops := genOps(rng, 40)
				s := row.open(t)
				seedHead(t, s)
				for i, o := range ops {
					c, err := row.apply(t, s, o)
					if err != nil {
						continue
					}
					if o.snapshot {
						if err := s.SwitchDLog(); err != nil {
							t.Fatalf("seed %d op %d: SwitchDLog: %v", seed, i, err)
						}
						if _, hc := headOf(s); hc != c {
							dropped++
							break
						}
					}
					head, hc := headOf(s)
					if hc != c {
						break
					}
					replay, rerr := s.replayBaselineAt(c)
					if rerr != nil {
						t.Fatalf("seed %d op %d: replay: %v", seed, i, rerr)
					}
					if nodeText(head) != nodeText(replay) {
						diverged++
						break
					}
				}
			}
			t.Logf("MATRIX %-7s seeds=%d dropped-at-snapshot=%d head-vs-replay=%d",
				row.name, seedCount(), dropped, diverged)
		})
	}
}
