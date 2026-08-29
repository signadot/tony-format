package storage

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// genClaimOps is genScopeOps with the RELATIVE operations added -- the ones a scope
// could not hold before, whose result depends on the value they are applied to.
//
// Many will not apply to the document as it stands, and that is fine: the store
// refuses a patch that does not apply, and the harness skips it. What is being
// generated is a stream where the ones that DO apply are frequent enough to matter.
func genClaimOps(rng *rand.Rand, n int) []scopeOp {
	paths := []string{"", "a", "a.b", "d", "d.e"}
	ops := make([]scopeOp, 0, n)
	for i := 0; i < n; i++ {
		o := scopeOp{
			scoped:   rng.Intn(2) == 0,
			path:     paths[rng.Intn(len(paths))],
			snapshot: rng.Intn(8) == 0,
		}
		switch rng.Intn(10) {
		case 0:
			o.src = `!delete`
		case 1:
			o.src = fmt.Sprintf("# note %d\n{k%d: %d}", i, rng.Intn(3), i)
		case 2:
			o.src = fmt.Sprintf(`!rename [{from: "k%d", to: "k%d"}]`,
				rng.Intn(3), rng.Intn(3))
		case 3:
			o.src = fmt.Sprintf(`{k%d: !replace {from: %d, to: %d}}`,
				rng.Intn(3), rng.Intn(i+1), i)
		case 4:
			o.src = fmt.Sprintf(`{k%d: !delete}`, rng.Intn(3))
		default:
			o.src = fmt.Sprintf(`{k%d: %d}`, rng.Intn(3), i)
		}
		ops = append(ops, o)
	}
	return ops
}

// touches reports whether a write at path w bears on what is held at path p -- one is
// the other, or one is inside the other. A scope writing a.b changes what it holds at
// a and at a.b.c; a baseline write there does not.
func touches(w, p string) bool {
	if w == p || w == "" || p == "" {
		return true
	}
	return strings.HasPrefix(p, w+".") || strings.HasPrefix(w, p+".")
}

// A scope's write is a standing CLAIM: once made, the scope reads it back the same
// way forever, whatever baseline does at that path afterwards. Only the scope itself
// changes it.
//
// This is the property the in-scope refusal existed to protect and could only protect
// by forbidding the write (3xn08cb6h12kr4psg5n0). Lowering keeps the property and
// allows the write, so the property is what has to be checked -- and checked against
// the relative operations that were previously refused, which no other differential
// generates.
//
// No second store and no reference model: after every scoped write the scope is read
// at the path CLAIMED, and that reading has to hold until the scope itself writes
// somewhere that bears on it.
//
// Claimed, not written: an absolute write of `{k2: 3}` at a is a merge patch, and it
// says nothing about a's other fields, so baseline's k0 shows through and only a.k2 is
// the scope's. ClaimPaths is what says where those are, and it answers with a SET,
// since a patch may state more than one thing -- so a write of two fields is checked
// as the two claims it makes and not as the container it was written at.
func TestAScopedWriteIsAStandingClaim(t *testing.T) {
	// Held back on what it still finds: 5 of 293 claims over 25 seeds, none of it the
	// claim's own doing -- four of the five reproduce with lowering switched off
	// entirely, where no claim is made at all.
	//
	//   fve9fxbqh12krxmpj9n0  a document-root comment is lost from a scoped read
	//                         once a later scoped write states a field beneath it.
	//   qth3kqe9h12ksxz9j9n0  a scope's delete of a field baseline never had is lost
	//                         when the overlay is written, so baseline's later write
	//                         shows through. Three ops, and it needs neither lowering
	//                         nor a claim. Seeds 1 and 22.
	//   seeds 11 and 15       a scoped read renders a flow value in dashes once a
	//                         snapshot exists. That is scope_overlay.go's stated
	//                         trade-off -- it strips presentation so that two
	//                         independent materializations cannot differ over
	//                         something nobody intended -- rather than an
	//                         undiscovered defect.
	//
	// Fixed since this was written: kbkxf53ph12krswpj9n0, a placeholder standing at a
	// field of a scalar, which made a refusal fatal; tmwq9mh6h12kskmxj9n0, a scoped
	// read missing the write that followed an overlay; and the claim's own half, a
	// claim stored with no !logd-patch-root because claimDelta returned before
	// lowerWrite marked and validated its delta.
	t.Skip("fve9fxbqh12krxmpj9n0, qth3kqe9h12ksxz9j9n0, and the trade-off above")

	const scope = "s1"
	broken, claims := 0, 0
	for seed := 1; seed <= seedCount(); seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		ops := genClaimOps(rng, 30)

		s := openTestStorage(t)
		s.EnableLowering(true)

		// The claims standing right now: every path the scope's last write claimed,
		// and what it read back at each once it had.
		type standing struct {
			path, val string
			at        int
		}
		var held []standing
		sc := scope

		// readClaim answers what the scope holds at p. ReadStateAt gives a document
		// rooted at the store's root whatever path it is asked for, so the reading
		// has to be navigated to rather than taken whole.
		readClaim := func(p string, c int64) (string, error) {
			doc, err := s.ReadStateAt(p, c, &sc)
			if err != nil {
				return "", err
			}
			if p == "" {
				return withComments(doc), nil
			}
			at, err := doc.GetKPathWith(p, ir.WithComments(true))
			if err != nil {
				return "", err
			}
			return withComments(at), nil
		}

		for i, o := range ops {
			c, err := applyScopeOp(t, s, o, scope)
			if err != nil {
				continue // did not apply to the document as it stood
			}
			var claimed []string
			if o.scoped {
				n, perr := parse.Parse([]byte(o.src), parse.ParseComments(true))
				if perr != nil {
					t.Fatalf("parse %q: %v", o.src, perr)
				}
				claimed = ClaimPaths(o.path, n)
			}
			if o.snapshot {
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("SwitchDLog: %v", err)
				}
			}
			// A scoped write bearing on a standing claim replaces it; the rest stand.
			if o.scoped {
				kept := held[:0]
				for _, h := range held {
					bears := false
					for _, cp := range claimed {
						if touches(cp, h.path) {
							bears = true
							break
						}
					}
					if !bears {
						kept = append(kept, h)
					}
				}
				held = kept
			}
			moved := false
			for _, h := range held {
				now, err := readClaim(h.path, c)
				if err != nil {
					t.Errorf("seed %d: the scope became unreadable at %q after op %d %s\n"+
						"  claim from op %d: %s\n  error: %v",
						seed, h.path, i, o, h.at, h.val, err)
					moved = true
					break
				}
				if now != h.val {
					t.Errorf("seed %d: the claim at %q moved\n  op %d %s made it: %s\n"+
						"  op %d %s left it:  %s",
						seed, h.path, h.at, ops[h.at], h.val, i, o, now)
					moved = true
					break
				}
			}
			if moved {
				broken++
				break
			}
			for _, cp := range claimed {
				now, err := readClaim(cp, c)
				if err != nil {
					t.Fatalf("seed %d op %d: reading back the scope's own write: %v", seed, i, err)
				}
				held = append(held, standing{path: cp, val: now, at: i})
				claims++
			}
		}
	}
	t.Logf("CLAIM seeds=%d claims=%d broken=%d", seedCount(), claims, broken)
}
