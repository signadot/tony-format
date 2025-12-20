package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
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
		return fmt.Errorf("%w: usage: git issue reopen <id>", cli.ErrUsage)
	}

	id, err := issuelib.ParseID(args[0])
	if err != nil {
		return err
	}

	// Get issue (must be closed)
	ref := issuelib.ClosedRefForID(id)
	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return fmt.Errorf("issue not found or not closed: %s", issuelib.FormatID(id))
	}

	// Update status
	issue.Status = "open"
	issue.ClosedBy = nil

	if err := cfg.store.Update(issue, "reopen", nil); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Move ref from refs/closed/ to refs/issues/
	newRef := issuelib.RefForID(id)
	if err := cfg.store.MoveRef(ref, newRef); err != nil {
		return fmt.Errorf("failed to move issue ref: %w", err)
	}

	fmt.Fprintf(cc.Out, "Reopened issue #%s\n", issuelib.FormatID(id))
	return nil
}
