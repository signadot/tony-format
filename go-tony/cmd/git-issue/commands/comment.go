package commands

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
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

	// Get comment text
	var commentText string
	if len(args) > 1 {
		commentText = strings.Join(args[1:], " ")
	} else if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		// stdin is a pipe/file, read from it
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

	if strings.TrimSpace(commentText) == "" {
		return fmt.Errorf("comment cannot be empty")
	}

	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return err
	}

	// Store the comment under a collision-free, content-addressed name
	// (discussion/<ts>-<hash>.md) so independent clones adding different comments
	// never overwrite each other on merge — the old count-based numbering did.
	now := time.Now()
	commentContent := commentBody(now, commentText)
	commentFile := commentFileName(now, commentContent)
	extraFiles := map[string]string{
		commentFile: commentContent,
	}

	// Create commit message
	firstLine := strings.Split(commentText, "\n")[0]
	if len(firstLine) > 60 {
		firstLine = firstLine[:57] + "..."
	}
	commitMsg := fmt.Sprintf("comment: %s", firstLine)

	if err := cfg.store.Update(issue, commitMsg, extraFiles); err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}

	fmt.Fprintf(cc.Out, "Added comment to issue %s (%s)\n", issue.ID, strings.TrimPrefix(commentFile, "discussion/"))
	return nil
}
