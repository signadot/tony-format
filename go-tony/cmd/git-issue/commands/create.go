package commands

import (
	"bufio"
	"fmt"
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

	// Prompt for description
	fmt.Fprintf(cc.Out, "Creating issue: %s\n", title)
	fmt.Fprintln(cc.Out, "Enter description (end with Ctrl+D):")
	fmt.Fprintln(cc.Out)

	var descLines []string
	descLines = append(descLines, "# "+title)
	descLines = append(descLines, "")

	scanner := bufio.NewScanner(cc.In)
	for scanner.Scan() {
		descLines = append(descLines, scanner.Text())
	}

	description := strings.Join(descLines, "\n")

	// Create issue
	issue, err := cfg.store.Create(title, description)
	if err != nil {
		return fmt.Errorf("failed to create issue: %w", err)
	}

	fmt.Fprintf(cc.Out, "\nCreated issue #%s\n", issuelib.FormatID(issue.ID))
	fmt.Fprintf(cc.Out, "Ref: %s\n", issue.Ref)

	return nil
}
