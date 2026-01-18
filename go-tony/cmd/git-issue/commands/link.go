package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
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

	xidrOrPrefix := args[0]

	commitSHA, err := cfg.store.VerifyCommit(args[1])
	if err != nil {
		return err
	}

	// Get issue (open or closed)
	ref, err := cfg.store.FindRef(xidrOrPrefix)
	if err != nil {
		return fmt.Errorf("issue not found: %s", xidrOrPrefix)
	}
	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return fmt.Errorf("failed to read issue: %w", err)
	}

	// Add commit if not already there
	if !issuelib.Contains(issue.Commits, commitSHA) {
		issue.Commits = append(issue.Commits, commitSHA)

		commitMsg := fmt.Sprintf("link: %s", commitSHA[:7])
		if err := cfg.store.Update(issue, commitMsg, nil); err != nil {
			return fmt.Errorf("failed to update issue: %w", err)
		}
	}

	// Add git note (reverse index)
	_ = cfg.store.AddNote(commitSHA, issue.ID)

	fmt.Fprintf(cc.Out, "Linked issue %s to commit %s\n", issue.ID, commitSHA[:7])
	return nil
}
