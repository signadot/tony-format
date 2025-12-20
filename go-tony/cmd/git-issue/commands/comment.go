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
		return fmt.Errorf("%w: usage: git issue comment <id> [text]", cli.ErrUsage)
	}

	id, err := issuelib.ParseID(args[0])
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
		// Open editor
		initialContent := `
# Enter your comment above.
# Lines starting with # will be ignored.
# Save and close the editor to submit, or leave empty to cancel.
`
		var err error
		commentText, err = issuelib.EditInEditor(initialContent)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
	}

	if strings.TrimSpace(commentText) == "" {
		return fmt.Errorf("comment cannot be empty")
	}

	// Find issue (open or closed)
	ref, err := cfg.store.FindRef(id)
	if err != nil {
		return err
	}

	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return err
	}

	// Count existing comments to get next number
	commentNum := cfg.countComments(ref) + 1

	// Create comment content
	timestamp := time.Now().Format(time.RFC3339)
	commentContent := fmt.Sprintf("<!-- Comment %03d - %s -->\n\n%s\n", commentNum, timestamp, commentText)

	// Prepare update
	commentFile := fmt.Sprintf("discussion/%03d.md", commentNum)
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

	fmt.Fprintf(cc.Out, "Added comment #%03d to issue #%s\n", commentNum, issuelib.FormatID(id))
	return nil
}

func (cfg *commentConfig) countComments(ref string) int {
	// Count .md files in discussion directory
	content, err := cfg.store.ReadFile(ref, "discussion")
	if err != nil {
		return 0
	}

	count := 0
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
