package matchcost

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/libctl"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"

	"github.com/signadot/verse/entity/entitytest"
)

func pat(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseTony())
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}
	return n
}

// A match's cost against the size of what stands at its path, for patterns that ask about the
// path's own shape and name nothing beneath it, one child, or everything.
func TestMatchCostAgainstChildren(t *testing.T) {
	ctx := context.Background()
	for _, children := range []int{10, 3000} {
		s := libctl.NewLogdSession(&libctl.LogdSessionConfig{Addr: entitytest.Addr(t), ClientID: "matchcost"})
		if err := s.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for i := 0; i < children; i++ {
			body := pat(t, `{owner: someone, grants: [{read: ["*"]}]}`)
			if _, err := s.PatchWith(ctx, fmt.Sprintf("big.kind.e%d", i), body, libctl.PatchOpts{}); err != nil {
				t.Fatal(err)
			}
		}
		for _, c := range []struct{ label, path, pattern string }{
			{"object-or-null at the parent", "big.kind", `!or [{}, !irtype null]`},
			{"one named child absent", "big.kind", `!not {nope: !not.or [{}, !irtype null]}`},
			{"one named child present", "big.kind", `{e5: {}}`},
			{"no pattern at the parent", "big.kind", ``},
			{"object-or-null at one child", "big.kind.e5", `!or [{}, !irtype null]`},
		} {
			const n = 20
			var p *ir.Node
			if c.pattern != "" {
				p = pat(t, c.pattern)
			}
			t0 := time.Now()
			var body *ir.Node
			for i := 0; i < n; i++ {
				b, err := s.MatchPattern(ctx, c.path, p)
				if err != nil {
					t.Logf("%s: %v", c.label, err)
					break
				}
				body = b
			}
			matchMs := float64(time.Since(t0).Microseconds()) / n / 1000
			fields := -1
			if body != nil {
				fields = len(body.Fields)
			}
			// The same pattern as a WRITE's precondition, which returns no body: what verse asks.
			t1 := time.Now()
			for i := 0; i < n; i++ {
				_, err := s.PatchWith(ctx, fmt.Sprintf("big.side.w%d", i), pat(t, `{x: 1}`),
					libctl.PatchOpts{Match: &logdapi.PathData{Path: c.path, Data: p}})
				if err != nil && !errors.Is(err, libctl.ErrMatchFailed) {
					t.Logf("%s as precondition: %v", c.label, err)
					break
				}
			}
			writeMs := float64(time.Since(t1).Microseconds()) / n / 1000
			t.Logf("%5d children  %-32s match %7.2fms (body fields %5d)   as a write's precondition %7.2fms", children, c.label, matchMs, fields, writeMs)
		}
	}
}
