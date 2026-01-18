package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
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

	xidrOrPrefix := args[0]

	// Verify closing commit if provided
	var closingCommit *string
	if cfg.Commit != "" {
		sha, err := cfg.store.VerifyCommit(cfg.Commit)
		if err != nil {
			return err
		}
		closingCommit = &sha
	}

	// Get issue (must be open) - try open refs only
	ref, err := cfg.store.FindRef(xidrOrPrefix)
	if err != nil {
		return fmt.Errorf("issue not found: %s", xidrOrPrefix)
	}
	if issuelib.IsClosedRef(ref) {
		return fmt.Errorf("issue already closed: %s", xidrOrPrefix)
	}

	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return fmt.Errorf("failed to read issue: %w", err)
	}

	// Update status
	issue.Status = "closed"
	issue.ClosedBy = closingCommit

	// Create commit message
	commitMsg := "close"
	if closingCommit != nil {
		commitMsg = fmt.Sprintf("close: closed by %s", (*closingCommit)[:7])
	}

	if err := cfg.store.Update(issue, commitMsg, nil); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Move ref from refs/issues/ to refs/closed/
	newRef := issuelib.ClosedRefForXIDR(issue.ID)
	if err := cfg.store.MoveRef(ref, newRef); err != nil {
		return fmt.Errorf("failed to move issue ref: %w", err)
	}

	fmt.Fprintf(cc.Out, "Closed issue %s\n", issue.ID)
	if closingCommit != nil {
		fmt.Fprintf(cc.Out, "Closed by: %s\n", (*closingCommit)[:7])
	}

	return nil
}
