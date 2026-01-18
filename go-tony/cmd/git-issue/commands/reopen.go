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
		return fmt.Errorf("%w: usage: git issue reopen <xidr>", cli.ErrUsage)
	}

	xidrOrPrefix := args[0]

	// Find the issue - must be closed
	ref, err := cfg.store.FindRef(xidrOrPrefix)
	if err != nil {
		return fmt.Errorf("issue not found: %s", xidrOrPrefix)
	}
	if !issuelib.IsClosedRef(ref) {
		return fmt.Errorf("issue is not closed: %s", xidrOrPrefix)
	}

	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return fmt.Errorf("failed to read issue: %w", err)
	}

	// Update status
	issue.Status = "open"
	issue.ClosedBy = nil

	if err := cfg.store.Update(issue, "reopen", nil); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Move ref from refs/closed/ to refs/issues/
	newRef := issuelib.RefForXIDR(issue.ID)
	if err := cfg.store.MoveRef(ref, newRef); err != nil {
		return fmt.Errorf("failed to move issue ref: %w", err)
	}

	fmt.Fprintf(cc.Out, "Reopened issue %s\n", issuelib.FormatID(issue.ID))
	return nil
}
