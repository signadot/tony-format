package txpool

import (
	"context"
	"testing"
	"time"

	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func startLogd(t *testing.T) *logdserver.Server {
	t.Helper()
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := logdserver.New(&logdserver.Spec{Storage: store})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start logd: %v", err)
	}
	t.Cleanup(func() { srv.StopTCP() })
	return srv
}

func waitStat(t *testing.T, p *Pool, participants, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Stats()[participants] == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pool[%d] = %d, want %d", participants, p.Stats()[participants], want)
}

// TestPool_Backfill proves the pool refills in the background as it is drained,
// so Gets keep being served from cache rather than each paying a logd round trip.
func TestPool_Backfill(t *testing.T) {
	logd := startLogd(t)
	p := New(&Config{LogdAddr: logd.TCPAddr(), PoolSize: 5})
	defer p.Close()

	ctx := context.Background()
	if err := p.Connect(ctx); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	p.Prefetch(ctx, 2)
	waitStat(t, p, 2, 5) // warmed to poolSize

	// Draining one is refilled back to poolSize in the background.
	if _, err := p.Get(ctx, 2); err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	waitStat(t, p, 2, 5)

	// Sustained draining stays served and converges back to poolSize.
	for i := 0; i < 8; i++ {
		if _, err := p.Get(ctx, 2); err != nil {
			t.Fatalf("Get %d failed: %v", i, err)
		}
	}
	waitStat(t, p, 2, 5)
}
