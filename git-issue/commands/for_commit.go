package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type forCommitConfig struct {
	*cli.Command
	store issuelib.Store
}

// ForCommitCommand returns the for-commit subcommand.
func ForCommitCommand(store issuelib.Store) *cli.Command {
	cfg := &forCommitConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "for-commit").
		WithSynopsis("for-commit <commit> - Show issues linked to commit").
		WithRun(cfg.run)
}

func (cfg *forCommitConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue for-commit <commit>", cli.ErrUsage)
	}

	sha, linked, err := ops.ForCommit(cfg.store, args[0])
	if err != nil {
		return err
	}
	if len(linked) == 0 {
		fmt.Fprintf(cc.Out, "No issues linked to commit %s\n", sha[:7])
		return nil
	}

	commitInfo, _ := cfg.store.GetCommitInfo(sha)
	fmt.Fprintf(cc.Out, "Issues linked to commit %s:\n\n", commitInfo)
	for _, l := range linked {
		writeLinked(cc, l)
	}
	return nil
}

// writeLinked is one line for an issue another names: its id, status and title,
// or what stopped it being read.
func writeLinked(cc *cli.Context, l ops.Linked) {
	if l.Err != "" {
		fmt.Fprintf(cc.Out, "  %s (%s)\n", l.ID, l.Err)
		return
	}
	fmt.Fprintf(cc.Out, "  %s %s[%s]%s %s\n",
		issuelib.FormatID(l.ID),
		issuelib.StatusColor(l.Status),
		l.Status,
		issuelib.ColorReset,
		l.Title,
	)
}
