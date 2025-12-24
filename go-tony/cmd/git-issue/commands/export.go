package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type exportConfig struct {
	*cli.Command
	store issuelib.Store
}

// ExportCommand returns the export subcommand.
func ExportCommand(store issuelib.Store) *cli.Command {
	cfg := &exportConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "export").
		WithSynopsis("export <id> [dir] - Export issue to directory").
		WithRun(cfg.run)
}

func (cfg *exportConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue export <id> [dir]", cli.ErrUsage)
	}

	id, err := issuelib.ParseID(args[0])
	if err != nil {
		return err
	}

	// Default directory is the formatted ID
	dir := issuelib.FormatID(id)
	if len(args) > 1 {
		dir = args[1]
	}

	ref, err := cfg.store.FindRef(id)
	if err != nil {
		return err
	}

	// Get current commit SHA for breadcrumb
	commit, err := cfg.store.GetRefCommit(ref)
	if err != nil {
		return err
	}

	// Create directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Export all files recursively
	if err := cfg.exportDir(ref, "", dir); err != nil {
		return err
	}

	// Write breadcrumb file
	breadcrumb := fmt.Sprintf("ref: %s\ncommit: %s\nexported: %s\n",
		ref, commit, time.Now().Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(dir, ".git-issue"), []byte(breadcrumb), 0644); err != nil {
		return fmt.Errorf("failed to write .git-issue: %w", err)
	}

	fmt.Fprintf(cc.Out, "Exported issue #%s to %s/\n", issuelib.FormatID(id), dir)
	return nil
}

// ExportToTempDir exports an issue to a temporary directory and returns the path.
// The caller is responsible for cleaning up the directory when done.
func ExportToTempDir(store issuelib.Store, ref string) (string, error) {
	gitStore, ok := store.(*issuelib.GitStore)
	if !ok {
		return "", fmt.Errorf("export requires GitStore")
	}

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "git-issue-context-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Export all files
	if err := exportDirRecursive(gitStore, store, ref, "", tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}

	return tmpDir, nil
}

// exportDirRecursive exports a directory tree from a git ref to a destination.
func exportDirRecursive(gitStore *issuelib.GitStore, store issuelib.Store, ref, path, destDir string) error {
	entries, err := gitStore.ListDir(ref, path)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", path, err)
	}

	for name, entry := range entries {
		srcPath := name
		if path != "" {
			srcPath = path + "/" + name
		}
		destPath := filepath.Join(destDir, name)

		typ := strings.Split(entry, ":")[0]

		if typ == "tree" {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			if err := exportDirRecursive(gitStore, store, ref, srcPath, destPath); err != nil {
				return err
			}
		} else if typ == "blob" {
			content, err := store.ReadFile(ref, srcPath)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", srcPath, err)
			}
			if err := os.WriteFile(destPath, content, 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", destPath, err)
			}
		}
	}

	return nil
}

func (cfg *exportConfig) exportDir(ref, path, destDir string) error {
	gitStore, ok := cfg.store.(*issuelib.GitStore)
	if !ok {
		return fmt.Errorf("export requires GitStore")
	}

	entries, err := gitStore.ListDir(ref, path)
	if err != nil {
		return fmt.Errorf("failed to list %s: %w", path, err)
	}

	for name, entry := range entries {
		srcPath := name
		if path != "" {
			srcPath = path + "/" + name
		}
		destPath := filepath.Join(destDir, name)

		typ := strings.Split(entry, ":")[0]

		if typ == "tree" {
			// Create subdirectory and recurse
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", destPath, err)
			}
			if err := cfg.exportDir(ref, srcPath, destPath); err != nil {
				return err
			}
		} else if typ == "blob" {
			// Read and write file
			content, err := cfg.store.ReadFile(ref, srcPath)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", srcPath, err)
			}
			if err := os.WriteFile(destPath, content, 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", destPath, err)
			}
		}
	}

	return nil
}
