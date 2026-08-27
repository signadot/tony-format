package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestStorageConfig_ToStorageDurability(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *StorageConfig
		want    storage.Durability
		wantErr bool
	}{
		{"nil section", nil, storage.DurabilityOS, false},
		{"unset", &StorageConfig{}, storage.DurabilityOS, false},
		{"os", &StorageConfig{Durability: "os"}, storage.DurabilityOS, false},
		{"sync", &StorageConfig{Durability: "sync"}, storage.DurabilitySync, false},
		{"misspelled", &StorageConfig{Durability: "fsync"}, storage.DurabilityOS, true},
		{"wrong case", &StorageConfig{Durability: "Sync"}, storage.DurabilityOS, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ToStorageDurability()
			if (err != nil) != tt.wantErr {
				t.Fatalf("ToStorageDurability() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ToStorageDurability() = %v, want %v", got, tt.want)
			}
		})
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logd.tony")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadConfig_StorageDurability(t *testing.T) {
	path := writeConfig(t, "storage:\n  durability: sync\n")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Storage == nil {
		t.Fatal("storage section not parsed")
	}
	got, err := cfg.Storage.ToStorageDurability()
	if err != nil {
		t.Fatalf("ToStorageDurability: %v", err)
	}
	if got != storage.DurabilitySync {
		t.Errorf("durability = %v, want %v", got, storage.DurabilitySync)
	}
}

// A misspelled durability must fail the load rather than quietly run with the
// default — the operator would not find out until a crash.
func TestLoadConfig_RejectsUnknownDurability(t *testing.T) {
	path := writeConfig(t, "storage:\n  durability: fsync\n")

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig accepted an unknown durability")
	}
	if !strings.Contains(err.Error(), "fsync") {
		t.Errorf("error does not name the offending value: %v", err)
	}
}

func TestLoadConfig_NoStorageSection(t *testing.T) {
	path := writeConfig(t, "tx:\n  timeout: 1s\n")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Storage != nil {
		t.Errorf("storage section = %+v, want nil when absent", cfg.Storage)
	}
	got, err := cfg.Storage.ToStorageDurability()
	if err != nil || got != storage.DurabilityOS {
		t.Errorf("absent section gave (%v, %v), want (%v, nil)", got, err, storage.DurabilityOS)
	}
}

func TestNew_AppliesStorageDurability(t *testing.T) {
	st, err := storage.Open(t.TempDir(), slog.Default())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer st.Close()

	New(&Spec{
		Config:  &Config{Storage: &StorageConfig{Durability: "sync"}},
		Storage: st,
		Log:     slog.Default(),
	})

	if got := st.GetDurability(); got != storage.DurabilitySync {
		t.Errorf("durability after New = %v, want %v", got, storage.DurabilitySync)
	}
}

// Without a storage section the server must leave the storage default in place.
func TestNew_LeavesDurabilityAloneWithoutSection(t *testing.T) {
	st, err := storage.Open(t.TempDir(), slog.Default())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer st.Close()

	New(&Spec{Config: &Config{}, Storage: st, Log: slog.Default()})

	if got := st.GetDurability(); got != storage.DurabilityOS {
		t.Errorf("durability after New = %v, want the default %v", got, storage.DurabilityOS)
	}
}

// A config file that says nothing about snapshots must still get a snapshot policy.
// It used to get none: LoadConfig returned the file as written, and a server with a
// nil Snapshot section never snapshots — so a file configuring a schema, and nothing
// else, silently left the delta log to grow forever (issue ps8kfs9dh12kr777fnn0).
func TestLoadConfig_SnapshotDefaultsWhenSectionAbsent(t *testing.T) {
	path := writeConfig(t, "tx:\n  timeout: 1s\n")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Snapshot == nil {
		t.Fatal("no snapshot policy: the log would grow without bound")
	}
	if cfg.Snapshot.MaxBytes != defaultSnapshotMaxBytes {
		t.Errorf("maxBytes = %d, want the default %d", cfg.Snapshot.MaxBytes, defaultSnapshotMaxBytes)
	}
	if cfg.Snapshot.MaxCommits != defaultSnapshotMaxCommits {
		t.Errorf("maxCommits = %d, want the default %d", cfg.Snapshot.MaxCommits, defaultSnapshotMaxCommits)
	}
	// The section the file DID write is its own.
	if cfg.Tx == nil || time.Duration(cfg.Tx.Timeout) != time.Second {
		t.Errorf("tx = %+v, want the configured 1s", cfg.Tx)
	}
}

// Written thresholds are the operator's, defaults included where they left one out.
func TestLoadConfig_SnapshotSectionIsTakenAsWritten(t *testing.T) {
	path := writeConfig(t, "snapshot:\n  maxBytes: 65536\n")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Snapshot.MaxBytes != 65536 {
		t.Errorf("maxBytes = %d, want 65536", cfg.Snapshot.MaxBytes)
	}
	// A present section is left exactly as written: zero here is "off", which is how
	// an operator turns a threshold off on purpose.
	if cfg.Snapshot.MaxCommits != 0 {
		t.Errorf("maxCommits = %d, want 0: the section was written without it", cfg.Snapshot.MaxCommits)
	}
}

// A config built in code can be as partial as one written in a file.
func TestNew_SnapshotDefaultsForPartialConfig(t *testing.T) {
	st, err := storage.Open(t.TempDir(), slog.Default())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer st.Close()

	cfg := &Config{Storage: &StorageConfig{Durability: "sync"}}
	srv := New(&Spec{Config: cfg, Storage: st, Log: slog.Default()})

	snap := srv.Spec.Config.Snapshot
	if snap == nil || snap.MaxBytes != defaultSnapshotMaxBytes {
		t.Fatalf("snapshot policy = %+v, want the default byte threshold", snap)
	}
}
