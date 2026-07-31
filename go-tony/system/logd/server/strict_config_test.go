package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/parse"
)

// Strictness has to work through the GENERATED decoders, not just the reflection path —
// every schemagen type has its own FromTonyIR, and that is what a config load goes
// through.
func decodeConfig(t *testing.T, src string, opts ...gomap.UnmapOption) error {
	t.Helper()
	node, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	cfg := &Config{}
	return cfg.FromTonyIR(node, opts...)
}

func TestStrictConfig_LenientByDefault(t *testing.T) {
	if err := decodeConfig(t, "storage:\n  durability: sync\nfutureSection:\n  x: 1\n"); err != nil {
		t.Errorf("default decode rejected an unknown section: %v", err)
	}
}

// The motivating case: a misspelled key currently decodes clean and the setting simply
// never takes effect, which is indistinguishable from having left it out.
func TestStrictConfig_RejectsMisspelledKey(t *testing.T) {
	err := decodeConfig(t, "storage:\n  durabilty: sync\n", gomap.Strict())
	if err == nil {
		t.Fatal("Strict() accepted a misspelled key")
	}
	if !strings.Contains(err.Error(), "durabilty") {
		t.Errorf("error does not name the offending key: %v", err)
	}
}

func TestStrictConfig_RejectsUnknownTopLevelSection(t *testing.T) {
	err := decodeConfig(t, "storage:\n  durability: sync\nnosuchsection:\n  x: 1\n", gomap.Strict())
	if err == nil {
		t.Fatal("Strict() accepted an unknown top-level section")
	}
	if !strings.Contains(err.Error(), "nosuchsection") {
		t.Errorf("error does not name the offending section: %v", err)
	}
}

func TestStrictConfig_AcceptsAValidConfig(t *testing.T) {
	src := "storage:\n  durability: sync\ntx:\n  timeout: 1000000000\nsnapshot:\n  maxCommits: 500\n"
	if err := decodeConfig(t, src, gomap.Strict()); err != nil {
		t.Errorf("Strict() rejected a config of only declared keys: %v", err)
	}
}

// LoadConfig is deliberately NOT strict yet: turning it on would reject configs that
// load today. This pins the current behavior so the change is a decision rather than a
// side effect.
func TestLoadConfig_NotStrictYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logd.tony")
	if err := os.WriteFile(path, []byte("storage:\n  durability: sync\nunknownKey: 1\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Errorf("LoadConfig rejected an unknown key; if that is now intended, update this test: %v", err)
	}
}
