package storage

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// scopeOp is one write in a mixed stream: baseline writes move the scope's base
// underneath it, which is the whole reason a scope cannot keep a relative operation.
type scopeOp struct {
	scoped   bool
	path     string
	src      string
	snapshot bool
}

func (o scopeOp) String() string {
	who := "baseline"
	if o.scoped {
		who = "scope"
	}
	s := fmt.Sprintf("%s %s <- %s", who, o.path, o.src)
	if o.snapshot {
		s += " [snapshot]"
	}
	return s
}

// genScopeOps mixes baseline and scoped writes over overlapping paths, so a scope
// write is regularly followed by a baseline write beneath, above or at it.
func genScopeOps(rng *rand.Rand, n int) []scopeOp {
	paths := []string{"", "a", "a.b", "d", "d.e"}
	ops := make([]scopeOp, 0, n)
	for i := 0; i < n; i++ {
		o := scopeOp{
			scoped:   rng.Intn(2) == 0,
			path:     paths[rng.Intn(len(paths))],
			snapshot: rng.Intn(8) == 0,
		}
		switch rng.Intn(8) {
		case 0:
			o.src = `!delete`
		case 1:
			o.src = fmt.Sprintf("# note %d\n{k%d: %d}", i, rng.Intn(3), i)
		default:
			o.src = fmt.Sprintf(`{k%d: %d}`, rng.Intn(3), i)
		}
		ops = append(ops, o)
	}
	return ops
}

func applyScopeOp(t *testing.T, s *Storage, o scopeOp, scope string) (int64, error) {
	t.Helper()
	n, err := parse.Parse([]byte(o.src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", o.src, err)
	}
	var sc *string
	if o.scoped {
		sc = &scope
	}
	txn, err := s.NewTx(1, sc)
	if err != nil {
		return 0, err
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: o.path, Data: n}})
	if err != nil {
		return 0, err
	}
	res := p.Commit()
	if !res.Committed {
		return 0, res.Error
	}
	return res.Commit, nil
}

// The scoped differential the baseline one cannot see: every number behind turning
// lowering on came from streams committed with NewTx(1, nil), so the scope path had
// none of it.
//
// Two stores, the same mixed stream of baseline and scoped writes, one lowering and
// one not. A scope read must answer the same in both at every commit -- lowering
// changes what the log KEEPS and must not change what it says.
func TestLoweringScopeDifferential(t *testing.T) {
	t.Skip("nm5r3sxah12ks2zmj5n0: the overlay's owned-path union merges into an operand")

	const scope = "s1"
	diverged := 0
	for seed := 1; seed <= seedCount(); seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		ops := genScopeOps(rng, 30)

		plain := openTestStorage(t)
		plain.EnableLowering(false)
		low := openTestStorage(t)
		low.EnableLowering(true)

		for i, o := range ops {
			cp, ep := applyScopeOp(t, plain, o, scope)
			cl, el := applyScopeOp(t, low, o, scope)
			if (ep == nil) != (el == nil) {
				t.Errorf("seed %d op %d %s: one store took the write and the other did not\n plain: %v\n low:   %v",
					seed, i, o, ep, el)
				diverged++
				break
			}
			if ep != nil {
				continue
			}
			if o.snapshot {
				if err := plain.SwitchDLog(); err != nil {
					t.Fatalf("plain SwitchDLog: %v", err)
				}
				if err := low.SwitchDLog(); err != nil {
					t.Fatalf("low SwitchDLog: %v", err)
				}
			}
			sc := scope
			wantScoped, err := plain.ReadStateAt("", cp, &sc)
			if err != nil {
				for j := 0; j <= i; j++ {
					t.Logf("  op %d %s", j, ops[j])
				}
				for _, seg := range plain.index.AllSegments() {
					if seg.KindedPath != "" {
						continue
					}
					e, rerr := plain.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile),
						seg.LogPosition, seg.LogFileGeneration)
					if rerr == nil && e.Patch != nil {
						who := "baseline"
						if seg.ScopeID != nil {
							who = "scope"
						}
						if seg.ScopeOverlay {
							who = "OVERLAY"
						}
						t.Logf("  %s entry@%d: %s", who, seg.EndCommit, withComments(e.Patch))
					}
				}
				t.Fatalf("seed %d op %d: plain scope read: %v", seed, i, err)
			}
			gotScoped, err := low.ReadStateAt("", cl, &sc)
			if err != nil {
				t.Fatalf("seed %d op %d: low scope read: %v", seed, i, err)
			}
			if withComments(wantScoped) != withComments(gotScoped) {
				t.Errorf("seed %d op %d %s: the scope reads differently\n plain: %s\n low:   %s",
					seed, i, o, withComments(wantScoped), withComments(gotScoped))
				diverged++
				break
			}
			// And baseline, which a scope write must not disturb.
			wantBase, err := plain.ReadStateAt("", cp, nil)
			if err != nil {
				t.Fatalf("seed %d op %d: plain baseline read: %v", seed, i, err)
			}
			gotBase, err := low.ReadStateAt("", cl, nil)
			if err != nil {
				t.Fatalf("seed %d op %d: low baseline read: %v", seed, i, err)
			}
			if withComments(wantBase) != withComments(gotBase) {
				t.Errorf("seed %d op %d %s: baseline reads differently\n plain: %s\n low:   %s",
					seed, i, o, withComments(wantBase), withComments(gotBase))
				diverged++
				break
			}
		}
	}
	t.Logf("SCOPE-DIFF seeds=%d diverged=%d", seedCount(), diverged)
}
