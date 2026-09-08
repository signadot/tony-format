package server

import (
	"os"
	"path/filepath"
	"testing"
)

// The same as logd's: a config file with nothing in it configures nothing rather than
// killing the process that reads it.
func TestAnEmptyConfigFileIsTheDefaults(t *testing.T) {
	for _, body := range []string{"", "# docd config\n", "\n\n", "# docd\n\n# tbd\n"} {
		p := filepath.Join(t.TempDir(), "docd.tony")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadConfig(p)
		if err != nil {
			t.Fatalf("LoadConfig(%q): %v", body, err)
		}
		if cfg == nil {
			t.Fatalf("LoadConfig(%q): nil config with no error", body)
		}
	}
}
