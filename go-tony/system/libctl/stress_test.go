package libctl

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestStress_ConcurrentGatesSharedSessionOverDocd mirrors verse's §8 pattern: N
// gates run concurrently, each holding a live Watch and issuing Match+Patch, all
// funneled through ONE shared session over docd. Reproduces (or rules out) the
// stall where routed replies go missing under concurrent request+watch load.
func TestStress_ConcurrentGatesSharedSessionOverDocd(t *testing.T) {
	t.Run("direct-logd", func(t *testing.T) {
		logd := startLogd(t)
		s := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "shared"})
		t.Cleanup(func() { s.Close() })
		stressGates(t, s)
	})
	t.Run("over-docd", func(t *testing.T) {
		logd := startLogd(t)
		docd := startDocdRouting(t, logd.TCPAddr())
		stressGates(t, docdClient(t, docd, "shared"))
	})
}

func stressGates(t *testing.T, sess *LogdSession) {
	ctx := context.Background()

	const gates = 8
	const iters = 60

	var wg sync.WaitGroup
	errCh := make(chan error, gates)
	stuck := make(chan string, gates)

	for g := 0; g < gates; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			path := fmt.Sprintf("gate.%d", g)

			if err := sess.Patch(ctx, path, vObj(0)); err != nil { // create the path first
				errCh <- fmt.Errorf("gate %d seed: %w", g, err)
				return
			}
			w, err := sess.Watch(ctx, path, nil)
			if err != nil {
				errCh <- fmt.Errorf("gate %d watch: %w", g, err)
				return
			}
			defer w.Close()
			go func() { //nolint:staticcheck // drain events
				for range w.Events() {
				}
			}()

			for i := 0; i < iters; i++ {
				done := make(chan error, 1)
				go func() {
					if _, e := sess.Match(ctx, path); e != nil {
						done <- e
						return
					}
					done <- sess.Patch(ctx, path, vObj(int64(i)))
				}()
				select {
				case e := <-done:
					if e != nil {
						errCh <- fmt.Errorf("gate %d iter %d: %w", g, i, e)
						return
					}
				case <-time.After(8 * time.Second):
					buf := make([]byte, 16<<20)
					n := runtime.Stack(buf, true)
					stuck <- fmt.Sprintf("gate %d iter %d WEDGED\n===STACKS===\n%s", g, i, buf[:n])
					return
				}
			}
		}(g)
	}

	fin := make(chan struct{})
	go func() { wg.Wait(); close(fin) }()

	select {
	case <-fin:
		select {
		case e := <-errCh:
			t.Fatalf("gate error: %v", e)
		default:
		}
	case s := <-stuck:
		t.Fatalf("STALL reproduced: %s", s)
	case e := <-errCh:
		t.Fatalf("gate error: %v", e)
	case <-time.After(60 * time.Second):
		t.Fatal("STALL reproduced: gates did not complete in 60s")
	}
}
