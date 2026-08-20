package libctl

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// The shape staging actually runs: a sensor reconciling a large set through docd,
// mountless, one small write per path -- not big documents. Writes were coming back as
// "context deadline exceeded" on an arbitrary ref, and the client logged answers
// arriving for requests it had already given up on, which says the writes were late
// rather than lost (dvgz9308h12ks4xmgdn0).
//
// What this measures is whether ONE small write gets slower as the set it lives in
// grows. If it does, a reconciler's own throughput falls as it succeeds, which is the
// failure that looks like a deadline on an unremarkable ref.
func TestSmallWriteCostAgainstSetSize(t *testing.T) {
	if testing.Short() {
		t.Skip("builds several stores")
	}
	ctx := context.Background()

	type point struct {
		entities int
		median   time.Duration
		worst    time.Duration
	}
	var points []point

	for _, entities := range []int{200, 1000, 3000} {
		store, err := storage.Open(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("open: %s", err)
		}
		srv := logdserver.New(&logdserver.Spec{Storage: store})
		if err := srv.StartTCP("127.0.0.1:0"); err != nil {
			t.Fatalf("start: %s", err)
		}
		session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "sensor"})
		if err := session.Connect(ctx); err != nil {
			t.Fatalf("connect: %s", err)
		}

		// The set, written one path at a time, the way a reconciler builds it.
		for i := 0; i < entities; i++ {
			id := "e" + strconv.Itoa(i)
			if _, err := session.Patch(ctx, "verse.entities."+id, ir.FromMap(map[string]*ir.Node{
				"id":     ir.FromString(id),
				"status": ir.FromString("ready"),
				"url":    ir.FromString("http://example/" + id),
			})); err != nil {
				t.Fatalf("seed write: %s", err)
			}
		}

		// Then the reconcile pass: one small write per path, timed individually.
		const sample = 100
		took := make([]time.Duration, 0, sample)
		for i := 0; i < sample; i++ {
			id := "e" + strconv.Itoa(i%entities)
			start := time.Now()
			if _, err := session.Patch(ctx, "verse.entities."+id, ir.FromMap(map[string]*ir.Node{
				"lastReflect": ir.FromString(fmt.Sprintf("2026-08-18T01:%02d:00Z", i%60)),
			})); err != nil {
				t.Fatalf("reconcile write: %s", err)
			}
			took = append(took, time.Since(start))
		}
		sort.Slice(took, func(a, b int) bool { return took[a] < took[b] })

		st := store.WriteStats()
		t.Logf("%5d entities: median %6s worst %6s | commits %d headMiss %d avg %s apply %s append %s index %s",
			entities, took[len(took)/2].Round(time.Microsecond), took[len(took)-1].Round(time.Millisecond),
			st.Commits, st.HeadMiss,
			(st.Total / time.Duration(st.Commits)).Round(time.Microsecond),
			(st.Apply / time.Duration(st.Commits)).Round(time.Microsecond),
			(st.Append / time.Duration(st.Commits)).Round(time.Microsecond),
			(st.Index / time.Duration(st.Commits)).Round(time.Microsecond))

		points = append(points, point{entities, took[len(took)/2], took[len(took)-1]})
		session.Close()
		srv.StopTCP()
		store.Close()
	}

	// A write of one small path should cost about the same whatever else is in the store.
	//
	// The bound is loose on purpose: this measures a round trip on a machine which may be
	// doing anything else at the time, and a test which fails on someone else's build
	// teaches people to re-run rather than to look. The tight version of this property is
	// asserted where it does not depend on the weather --
	// tony.TestFoldDoesNotAllocatePerField counts allocations, which are the same on any
	// machine.
	first, last := points[0], points[len(points)-1]
	if first.median > 5*time.Millisecond {
		t.Skipf("a small write takes %s here at %d entities; the machine is too busy to measure scaling against",
			first.median.Round(time.Microsecond), first.entities)
	}
	if last.median > 8*first.median {
		t.Errorf("a small write costs %s at %d entities against %s at %d: it scales with the set, not the patch",
			last.median.Round(time.Microsecond), last.entities,
			first.median.Round(time.Microsecond), first.entities)
	}
}
