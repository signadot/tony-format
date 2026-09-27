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

	// Every repository the command covers: the one it runs on, or outside a
	// repository the set, each line then saying which.
	repos := targets(cfg.store)
	found := false
	for _, r := range repos {
		// Outside a repository there are no refs to list, which "No issues
		// found" would say as if it were the answer.
		if err := r.Store.VerifyRepository(); err != nil {
			return err
		}
		issues, err := ops.List(r.Store, cfg.ShowAll, cfg.Label)
		if err != nil {
			return err
		}
		for _, issue := range issues {
			found = true
			if len(repos) > 1 {
				fmt.Fprint(cc.Out, r.Name+"  ")
			}
			fmt.Fprintln(cc.Out, issuelib.FormatOneLiner(issue))
		}
	}
	if !found {
		fmt.Fprintln(cc.Out, "No issues found")
	}
	return nil
}
