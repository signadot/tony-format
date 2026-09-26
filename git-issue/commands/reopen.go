package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type reopenConfig struct {
	*cli.Command
	store issuelib.Store
}

// ReopenCommand returns the reopen subcommand.
func ReopenCommand(store issuelib.Store) *cli.Command {
	cfg := &reopenConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "reopen").
		WithSynopsis("reopen <id> - Reopen a closed issue").
		WithRun(cfg.run)
}

func (cfg *reopenConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue reopen <xidr>", cli.ErrUsage)
	}

	issue, err := ops.Reopen(cfg.store, args[0])
	if err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Reopened issue %s\n", issuelib.FormatID(issue.ID))
	return nil
}
