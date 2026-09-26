package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type listConfig struct {
	*cli.Command
	store   issuelib.Store
	ShowAll bool   `cli:"name=all aliases=a desc='show all issues including closed'"`
	Label   string `cli:"name=label aliases=l desc='filter by label'"`
}

// ListCommand returns the list subcommand.
func ListCommand(store issuelib.Store) *cli.Command {
	cfg := &listConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "list").
		WithSynopsis("list [--all] [--label <label>] - List issues").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *listConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	issues, err := ops.List(cfg.store, cfg.ShowAll, cfg.Label)
	if err != nil {
		return err
	}
	if len(issues) == 0 {
		fmt.Fprintln(cc.Out, "No issues found")
		return nil
	}
	for _, issue := range issues {
		fmt.Fprintln(cc.Out, issuelib.FormatOneLiner(issue))
	}
	return nil
}
