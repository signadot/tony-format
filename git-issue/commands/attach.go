package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type attachConfig struct {
	*cli.Command
	store issuelib.Store
}

// AttachCommand returns the attach subcommand.
func AttachCommand(store issuelib.Store) *cli.Command {
	cfg := &attachConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "attach").
		WithSynopsis("attach <id> <path> - Attach file/directory to issue").
		WithRun(cfg.run)
}

func (cfg *attachConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: usage: git issue attach <xidr> <path>", cli.ErrUsage)
	}
	name, files, err := ops.ReadAttachment(args[1])
	if err != nil {
		return err
	}
	issue, paths, err := ops.Attach(cfg.store, args[0], name, files)
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "Attached %s (%d file(s)) to issue %s\n", name, len(paths), issue.ID)
	return nil
}
