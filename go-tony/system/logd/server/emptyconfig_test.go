package server

import (
	"os"
	"path/filepath"
	"testing"
)

// A config file an operator has emptied -- every setting commented out, or the file
// created and not yet filled in -- configures nothing, and that is the same answer as
// passing no config file at all: the defaults.
//
// It used to take the process down instead. Parsing such a file answers no document, and
// expansion clones the document it is given, so `o sys up` died on a nil dereference
// with no message an operator could act on -- on a file whose only content was a comment.
func TestAnEmptyConfigFileIsTheDefaults(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"comment only", "# logd config\n"},
		{"blank lines", "\n\n"},
		{"comments and blanks", "# logd\n\n# nothing set yet\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "logd.tony")
			if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(p)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg == nil {
				t.Fatal("nil config with no error: every caller that reads a field panics on it")
			}
			// The defaults a store gets with no config file at all.
			if cfg.Snapshot == nil || cfg.Snapshot.MaxCommits != defaultSnapshotMaxCommits {
				t.Errorf("snapshot policy is %+v, want the defaults", cfg.Snapshot)
			}
		})
	}
}
