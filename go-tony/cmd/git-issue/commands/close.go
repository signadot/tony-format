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
		return fmt.Errorf("%w: usage: git issue close <id> [--commit <sha>]", cli.ErrUsage)
	}

	id, err := issuelib.ParseID(args[0])
	if err != nil {
		return err
	}

	// Verify closing commit if provided
	var closingCommit *string
	if cfg.Commit != "" {
		sha, err := cfg.store.VerifyCommit(cfg.Commit)
		if err != nil {
			return err
		}
		closingCommit = &sha
	}

	// Get issue (must be open)
	ref := issuelib.RefForID(id)
	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return fmt.Errorf("issue not found or already closed: %s", issuelib.FormatID(id))
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
	newRef := issuelib.ClosedRefForID(id)
	if err := cfg.store.MoveRef(ref, newRef); err != nil {
		return fmt.Errorf("failed to move issue ref: %w", err)
	}

	fmt.Fprintf(cc.Out, "Closed issue #%s\n", issuelib.FormatID(id))
	if closingCommit != nil {
		fmt.Fprintf(cc.Out, "Closed by: %s\n", (*closingCommit)[:7])
	}

	return nil
}
