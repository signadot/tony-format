package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type createConfig struct {
	*cli.Command
	store issuelib.Store
}

// CreateCommand returns the create subcommand.
func CreateCommand(store issuelib.Store) *cli.Command {
	cfg := &createConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "create").
		WithSynopsis("create <title> - Create new issue").
		WithRun(cfg.run)
}

func (cfg *createConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue create <title>", cli.ErrUsage)
	}

	title := strings.Join(args, " ")

	// Get description text
	var descBody string
	if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		// stdin is a pipe/file, read from it
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		descBody = string(data)
	} else {
		// Open editor
		initialContent := fmt.Sprintf(`# %s

# Enter description above.
# Lines starting with # will be ignored.
# Save and close the editor to submit, or leave empty to cancel.
`, title)
		var err error
		descBody, err = issuelib.EditInEditor(initialContent)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
	}

	// Build full description with title as heading
	description := "# " + title + "\n\n" + strings.TrimSpace(descBody)

	if strings.TrimSpace(descBody) == "" {
		return fmt.Errorf("description cannot be empty")
	}

	// Create issue
	issue, err := cfg.store.Create(title, description)
	if err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	fmt.Fprintf(cc.Out, "Created issue #%s\n", issuelib.FormatID(issue.ID))
	fmt.Fprintf(cc.Out, "Ref: %s\n", issue.Ref)

	return nil
}
