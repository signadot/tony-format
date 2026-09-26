package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type linkConfig struct {
	*cli.Command
	store issuelib.Store
}

// LinkCommand returns the link subcommand.
func LinkCommand(store issuelib.Store) *cli.Command {
	cfg := &linkConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "link").
		WithSynopsis("link <id> <commit> - Link issue to commit").
		WithRun(cfg.run)
}

func (cfg *linkConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: usage: git issue link <xidr> <commit>", cli.ErrUsage)
	}

	issue, sha, err := ops.Link(cfg.store, args[0], args[1])
	if err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Linked issue %s to commit %s\n", issue.ID, sha[:7])
	return nil
}
