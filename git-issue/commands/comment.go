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

// CommentCommand returns the comment subcommand: a comment added, or with
// --edit, one changed in place.
//
// --edit is read by hand, and only right after the id, where the usage puts it:
// every other argument is the comment's text as it was given, so a comment may
// begin with a dash. An option parser would take "- a list item" for an option.
func CommentCommand(store issuelib.Store) *cli.Command {
	cfg := &commentConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "comment").
		WithSynopsis("comment <id> [--edit <comment>] [text] - Add a comment to an issue, or change one by its name as show prints it").
		WithRun(cfg.run)
}

func (cfg *commentConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue comment <xidr> [--edit <comment>] [text]", cli.ErrUsage)
	}

	xidrOrPrefix := args[0]
	if len(args) > 1 {
		name, value, hasValue := strings.Cut(args[1], "=")
		if name == "--edit" || name == "-e" {
			rest := args[2:]
			if !hasValue {
				if len(rest) == 0 {
					return fmt.Errorf("%w: --edit takes the comment to change", cli.ErrUsage)
				}
				value, rest = rest[0], rest[1:]
			}
			return cfg.edit(cc, xidrOrPrefix, value, rest)
		}
	}

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

	// ops resolves the id itself; the ref above was for the editor's context.
	issue, path, err := ops.Comment(cfg.store, xidrOrPrefix, commentText)
	if err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Added comment to issue %s (%s)\n", issue.ID, strings.TrimPrefix(path, "discussion/"))
	return nil
}

// edit changes the comment --edit names. The text is the arguments, or stdin
// when it is not a terminal, or the editor opened on the comment's text as
// stored, headings kept, as edit opens on the description.
func (cfg *commentConfig) edit(cc *cli.Context, id, comment string, args []string) error {
	var text string
	if len(args) > 0 {
		text = strings.Join(args, " ")
	} else if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		text = string(data)
	} else {
		entry, err := ops.FindComment(cfg.store, id, comment)
		if err != nil {
			return err
		}
		text, err = issuelib.EditTextInEditor(entry.Text)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
	}
	issue, path, err := ops.EditComment(cfg.store, id, comment, text)
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "Edited comment %s on issue %s\n", strings.TrimPrefix(path, "discussion/"), issue.ID)
	return nil
}
