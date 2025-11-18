package codegen

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverPackages discovers Go packages in the given directory.
// If recursive is true, it scans subdirectories recursively.
func DiscoverPackages(dir string, recursive bool) ([]*PackageInfo, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for %q: %w", dir, err)
	}

	var packages []*PackageInfo
	visited := make(map[string]bool)

	err = filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip if not a directory
		if !info.IsDir() {
			return nil
		}

		// Skip hidden directories and vendor
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || base == "vendor" {
			return filepath.SkipDir
		}

		// If not recursive, only process the root directory
		if !recursive && path != absDir {
			return filepath.SkipDir
		}

		// Check if this directory contains Go files
		pkg, err := build.ImportDir(path, 0)
		if err != nil {
			// Not a valid Go package, skip
			return nil
		}

		// Skip if no Go files
		if len(pkg.GoFiles) == 0 {
			return nil
		}

		// Avoid processing the same package twice
		if visited[pkg.ImportPath] {
			return nil
		}
		visited[pkg.ImportPath] = true

		// Build full file paths
		files := make([]string, 0, len(pkg.GoFiles))
		for _, f := range pkg.GoFiles {
			files = append(files, filepath.Join(path, f))
		}

		packages = append(packages, &PackageInfo{
			Path:  pkg.ImportPath,
			Dir:   path,
			Name:  pkg.Name,
			Files: files,
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory %q: %w", dir, err)
	}

	return packages, nil
}

// DiscoverFiles finds all .go files in a package directory.
// This is a convenience function that wraps DiscoverPackages for a single package.
func DiscoverFiles(pkgPath string) ([]string, error) {
	pkg, err := build.Import(pkgPath, "", 0)
	if err != nil {
		return nil, fmt.Errorf("failed to import package %q: %w", pkgPath, err)
	}

	files := make([]string, 0, len(pkg.GoFiles))
	for _, f := range pkg.GoFiles {
		files = append(files, filepath.Join(pkg.Dir, f))
	}

	return files, nil
}
