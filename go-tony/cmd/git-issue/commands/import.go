package commands

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type importConfig struct {
	*cli.Command
	store issuelib.Store
	Force bool `cli:"name=force desc='Overwrite even if issue was modified'"`
}

// ImportCommand returns the import subcommand.
func ImportCommand(store issuelib.Store) *cli.Command {
	cfg := &importConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "import").
		WithSynopsis("import [--force] <dir> - Import issue from directory").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *importConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue import <dir>", cli.ErrUsage)
	}

	dir := args[0]

	// Read breadcrumb
	breadcrumbPath := filepath.Join(dir, ".git-issue")
	breadcrumb, err := parseBreadcrumb(breadcrumbPath)
	if err != nil {
		return fmt.Errorf("failed to read .git-issue: %w", err)
	}

	// Check for conflicts
	currentCommit, err := cfg.store.GetRefCommit(breadcrumb.Ref)
	if err != nil {
		return fmt.Errorf("issue ref not found: %s", breadcrumb.Ref)
	}

	if currentCommit != breadcrumb.Commit && !cfg.Force {
		return fmt.Errorf("issue was modified since export\n  exported at: %s\n  current:     %s\nUse --force to overwrite",
			breadcrumb.Commit[:12], currentCommit[:12])
	}

	// Import the directory
	gitStore, ok := cfg.store.(*issuelib.GitStore)
	if !ok {
		return fmt.Errorf("import requires GitStore")
	}

	if err := cfg.importDir(gitStore, breadcrumb.Ref, dir); err != nil {
		return err
	}

	// Extract XIDR from ref for display
	xidr, _ := issuelib.XIDRFromRef(breadcrumb.Ref)
	fmt.Fprintf(cc.Out, "Imported issue %s from %s/\n", issuelib.FormatID(xidr), dir)
	return nil
}

type breadcrumb struct {
	Ref      string
	Commit   string
	Exported string
}

func parseBreadcrumb(path string) (*breadcrumb, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	bc := &breadcrumb{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ref: ") {
			bc.Ref = strings.TrimPrefix(line, "ref: ")
		} else if strings.HasPrefix(line, "commit: ") {
			bc.Commit = strings.TrimPrefix(line, "commit: ")
		} else if strings.HasPrefix(line, "exported: ") {
			bc.Exported = strings.TrimPrefix(line, "exported: ")
		}
	}

	if bc.Ref == "" || bc.Commit == "" {
		return nil, fmt.Errorf("invalid .git-issue file: missing ref or commit")
	}

	return bc, scanner.Err()
}

func (cfg *importConfig) importDir(gitStore *issuelib.GitStore, ref, dir string) error {
	// Collect all files from directory
	files := make(map[string][]byte) // path -> content

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip .git-issue breadcrumb
		if d.Name() == ".git-issue" {
			return nil
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", relPath, err)
		}

		files[relPath] = content
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	// Create tree and commit
	return gitStore.ReplaceTree(ref, "import: update from directory", files)
}
