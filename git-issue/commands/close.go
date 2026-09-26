package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type closeConfig struct {
	*cli.Command
	store  issuelib.Store
	Commit string `cli:"name=commit aliases=c desc='commit that closes this issue'"`
}

// CloseCommand returns the close subcommand.
func CloseCommand(store issuelib.Store) *cli.Command {
	cfg := &closeConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "close").
		WithSynopsis("close <id> [--commit <sha>] - Close issue").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *closeConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue close <xidr> [--commit <sha>]", cli.ErrUsage)
	}

	issue, err := ops.Close(cfg.store, args[0], cfg.Commit)
	if err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Closed issue %s\n", issue.ID)
	if issue.ClosedBy != nil {
		fmt.Fprintf(cc.Out, "Closed by: %s\n", (*issue.ClosedBy)[:7])
	}

	return nil
}
