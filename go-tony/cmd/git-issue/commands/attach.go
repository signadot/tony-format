package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type attachConfig struct {
	*cli.Command
	store issuelib.Store
}

// AttachCommand returns the attach subcommand.
func AttachCommand(store issuelib.Store) *cli.Command {
	cfg := &attachConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "attach").
		WithSynopsis("attach <id> <path> - Attach file/directory to issue").
		WithRun(cfg.run)
}

func (cfg *attachConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: usage: git issue attach <id> <path>", cli.ErrUsage)
	}

	id, err := issuelib.ParseID(args[0])
	if err != nil {
		return err
	}

	attachPath := args[1]
	info, err := os.Stat(attachPath)
	if err != nil {
		return fmt.Errorf("path not found: %s", attachPath)
	}

	// Find issue
	ref, err := cfg.store.FindRef(id)
	if err != nil {
		return err
	}

	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return err
	}

	// Collect files to attach
	extraFiles := make(map[string]string)
	baseName := filepath.Base(attachPath)

	if info.IsDir() {
		err = filepath.WalkDir(attachPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				content, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("failed to read %s: %w", path, err)
				}
				relPath, _ := filepath.Rel(attachPath, path)
				discussionPath := filepath.Join("discussion/files", baseName, relPath)
				extraFiles[discussionPath] = string(content)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to walk directory: %w", err)
		}
	} else {
		content, err := os.ReadFile(attachPath)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		discussionPath := filepath.Join("discussion/files", baseName)
		extraFiles[discussionPath] = string(content)
	}

	// Update issue
	fileCount := len(extraFiles)
	commitMsg := fmt.Sprintf("attach: %s (%d file(s))", baseName, fileCount)

	if err := cfg.store.Update(issue, commitMsg, extraFiles); err != nil {
		return fmt.Errorf("failed to attach files: %w", err)
	}

	fmt.Fprintf(cc.Out, "Attached %s (%d file(s)) to issue #%s\n", baseName, fileCount, issuelib.FormatID(id))
	return nil
}
