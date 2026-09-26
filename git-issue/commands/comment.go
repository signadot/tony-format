package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type commentConfig struct {
	*cli.Command
	store issuelib.Store
}

// CommentCommand returns the comment subcommand.
func CommentCommand(store issuelib.Store) *cli.Command {
	cfg := &commentConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "comment").
		WithSynopsis("comment <id> [text] - Add comment to issue").
		WithRun(cfg.run)
}

func (cfg *commentConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue comment <xidr> [text]", cli.ErrUsage)
	}

	xidrOrPrefix := args[0]

	// Find issue first (needed for context export)
	ref, err := cfg.store.FindRef(xidrOrPrefix)
	if err != nil {
		return err
	}

	// The text: the arguments, or stdin when it is not a terminal, or the editor.
	var commentText string
	if len(args) > 1 {
		commentText = strings.Join(args[1:], " ")
	} else if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		commentText = string(data)
	} else {
		// Export issue to temp directory for context
		contextDir, err := ExportToTempDir(cfg.store, ref)
		if err != nil {
			// Non-fatal: warn but continue without context
			fmt.Fprintf(cc.Err, "Warning: could not export issue context: %v\n", err)
			contextDir = ""
		}
		if contextDir != "" {
			defer os.RemoveAll(contextDir)
		}

		// Open editor with context information
		initialContent := "\n# Enter your comment above.\n# Lines starting with # will be ignored.\n# Save and close the editor to submit, or leave empty to cancel.\n"
		if contextDir != "" {
			initialContent = "\n# Issue context in current directory (existing comments in ./discussion/)\n#\n# Enter your comment above.\n# Lines starting with # will be ignored.\n# Save and close the editor to submit, or leave empty to cancel.\n"
		}
		commentText, err = issuelib.EditInEditorWithDir(initialContent, contextDir)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
	}

	issue, path, err := ops.Comment(cfg.store, ref, commentText)
	if err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Added comment to issue %s (%s)\n", issue.ID, strings.TrimPrefix(path, "discussion/"))
	return nil
}
