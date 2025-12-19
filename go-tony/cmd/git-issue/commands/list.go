package commands

import (
	"fmt"
	"sort"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type listConfig struct {
	*cli.Command
	store   issuelib.Store
	ShowAll bool `cli:"name=all aliases=a desc='show all issues including closed'"`
}

// ListCommand returns the list subcommand.
func ListCommand(store issuelib.Store) *cli.Command {
	cfg := &listConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "list").
		WithSynopsis("list [--all] - List issues").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *listConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	issues, err := cfg.store.List(cfg.ShowAll)
	if err != nil {
		return fmt.Errorf("failed to list issues: %w", err)
	}

	if len(issues) == 0 {
		fmt.Fprintln(cc.Out, "No issues found")
		return nil
	}

	// Sort by ID (descending)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ID > issues[j].ID
	})

	// Print issues
	for _, issue := range issues {
		fmt.Fprintln(cc.Out, issuelib.FormatOneLiner(issue))
	}

	return nil
}
