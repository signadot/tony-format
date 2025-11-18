package codegen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPackages(t *testing.T) {
	// Test discovering the current package
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Go up to the gomap directory (we're in gomap/codegen)
	gomapDir := filepath.Dir(wd)

	packages, err := DiscoverPackages(gomapDir, false)
	if err != nil {
		t.Fatalf("failed to discover packages: %v", err)
	}

	if len(packages) == 0 {
		t.Error("expected at least one package")
	}

	// Check that we found the gomap package
	found := false
	for _, pkg := range packages {
		if pkg.Name == "gomap" {
			found = true
			if len(pkg.Files) == 0 {
				t.Error("expected package to have files")
			}
			break
		}
	}

	if !found {
		t.Error("expected to find gomap package")
	}
}
