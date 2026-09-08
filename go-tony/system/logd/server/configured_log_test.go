package server

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// What a store says it is configured with must be what it is running: a config's zero
// field means "the default", the setter resolves it, and a line built from the FILE
// prints a 0 where an 8 is in force. An operator reading `slotsPerTier: 0` in the log has
// been told something untrue of the store, which is worse than not being told -- and it
// is the kind of untrue that survives, because nothing downstream can contradict it.
//
// So every configured-X line is asked of the STORE. Here one field of each section is
// named and the rest left to default, which is what a real config looks like.
func TestTheConfiguredLinesSayWhatIsInForce(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	var logged bytes.Buffer
	New(&Spec{
		Storage: store,
		Log:     slog.New(slog.NewTextHandler(&logged, nil)),
		Config: &Config{
			Storage:    &StorageConfig{PathSnapshotTail: 32},
			Compaction: &CompactionConfig{Cutoff: Duration(2 * time.Hour)},
		},
	})
	log := logged.String()

	for _, want := range []string{
		// The compaction policy in force, whole: the named cutoff and the four
		// defaults the file left out.
		`msg="configured compaction"`,
		"cutoff=2h",
		"baseInterval=1h",
		"slotsPerTier=8",
		"multiplier=2",
		"gracePeriod=5s",
		// The path-snapshot policy in force: the named tail and the default budget.
		`msg="configured path snapshots"`,
		"tail=32",
		"bytes=1048576",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not say %s:\n%s", want, log)
		}
	}
	for _, never := range []string{"slotsPerTier=0", "multiplier=0", "bytes=0", "tail=0"} {
		if strings.Contains(log, never) {
			t.Errorf("the log says %s, which is a file's empty field and not the store's policy:\n%s", never, log)
		}
	}

	// And the store agrees with what was logged.
	cfg := store.CompactionConfig()
	if cfg == nil || cfg.SlotsPerTier != 8 || cfg.Multiplier != 2 || cfg.Cutoff != 2*time.Hour {
		t.Errorf("the store's compaction policy is %+v", cfg)
	}
	if tail, bytes := store.PathSnapshotPolicy(); tail != 32 || bytes != storage.DefaultPathSnapshotBytes {
		t.Errorf("the store's path-snapshot policy is tail %d, bytes %d", tail, bytes)
	}
}
