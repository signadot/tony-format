// Command logd-patchif-perf measures the cost of a conditional patch (PatchIf) as a function
// of WHERE ITS MATCH IS ANCHORED, against a growing document.
//
// Why: verse writes every compare-and-swap through PatchIf with the match anchored at its
// ROOT path, because docd accepts a single match path and several preconditions may sit at
// different places. In the overwhelmingly common case there is exactly one precondition, at
// the write's own path — so the root anchoring is not needed, and the question this asks is
// what it costs.
//
//	go run .            # 200 writes per arm at 100/500/1000 pre-existing entities
//	go run . -n 500 -sizes 1000,5000
//
// It uses go-tony only: an in-process logd over a temp dir, one libctl session, no verse.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/libctl"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

const root = "verse"

func main() {
	n := flag.Int("n", 200, "writes per arm")
	sizes := flag.String("sizes", "100,500,1000", "pre-existing entities to fill with, per run")
	flag.Parse()

	fmt.Printf("%-10s %-34s %10s %12s\n", "filled", "arm", "writes/s", "µs/write")
	for _, f := range strings.Split(*sizes, ",") {
		fill, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			die(err)
		}
		run(fill, *n)
	}
}

// run brings up a fresh logd, fills it with `fill` entities, and times each arm.
func run(fill, n int) {
	dir, err := os.MkdirTemp("", "logd-perf-")
	if err != nil {
		die(err)
	}
	defer os.RemoveAll(dir)
	store, err := storage.Open(dir, nil)
	if err != nil {
		die(err)
	}
	defer store.Close()
	srv := logdserver.New(&logdserver.Spec{Storage: store})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		die(err)
	}
	defer srv.StopTCP()

	ctx := context.Background()
	sess := libctl.NewLogdSession(&libctl.LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "perf"})
	if err := sess.Connect(ctx); err != nil {
		die(err)
	}
	defer sess.Close()

	// Fill: unconditional patches, the shape a first write takes.
	for i := range fill {
		if _, err := sess.Patch(ctx, path(i), payload(i, "open")); err != nil {
			die(err)
		}
	}

	// Arm 1: no precondition at all — the floor.
	time1 := timed(n, func(i int) error {
		_, err := sess.Patch(ctx, path(i), payload(i, "a"))
		return err
	})
	report(fill, "Patch (no match)", n, time1)

	// Arm 2: precondition anchored at the ENTITY's own path — what one precondition needs.
	time2 := timed(n, func(i int) error {
		_, err := sess.PatchIf(ctx, path(i), payload(i, "b"),
			&api.PathData{Path: path(i), Data: payload(i, "a")})
		return err
	})
	report(fill, "PatchIf, match at the entity", n, time2)

	// Arm 3: the SAME precondition, nested under the root — what verse does today.
	time3 := timed(n, func(i int) error {
		_, err := sess.PatchIf(ctx, path(i), payload(i, "c"),
			&api.PathData{Path: root, Data: nestUnderRoot(i, payload(i, "b"))})
		return err
	})
	report(fill, "PatchIf, match at the root", n, time3)
	fmt.Println()
}

// path is the kpath verse writes an entity to: <root>.<system>.<kind>.<id>.
func path(i int) string { return fmt.Sprintf("%s.demo.thing.e%d", root, i) }

func payload(i int, stage string) *ir.Node {
	return ir.FromMap(map[string]*ir.Node{
		"stage": ir.FromString(stage),
		"repo":  ir.FromString("r"),
		"n":     ir.FromInt(int64(i)),
	})
}

// nestUnderRoot wraps one entity's pattern in the object shape that reaches it from the root
// — {demo: {thing: {e<i>: <pattern>}}} — which is what verse's rootPattern builds.
func nestUnderRoot(i int, pattern *ir.Node) *ir.Node {
	return ir.FromMap(map[string]*ir.Node{
		"demo": ir.FromMap(map[string]*ir.Node{
			"thing": ir.FromMap(map[string]*ir.Node{
				fmt.Sprintf("e%d", i): pattern,
			}),
		}),
	})
}

func timed(n int, do func(i int) error) time.Duration {
	start := time.Now()
	for i := range n {
		if err := do(i); err != nil {
			die(err)
		}
	}
	return time.Since(start)
}

func report(fill int, arm string, n int, d time.Duration) {
	fmt.Printf("%-10d %-34s %10.0f %12.0f\n", fill, arm,
		float64(n)/d.Seconds(), float64(d.Microseconds())/float64(n))
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "perf:", err)
	os.Exit(1)
}
