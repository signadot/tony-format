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
		return fmt.Errorf("%w: usage: git issue link <id> <commit>", cli.ErrUsage)
	}

	id, err := issuelib.ParseID(args[0])
	if err != nil {
		return err
	}

	commitSHA, err := cfg.store.VerifyCommit(args[1])
	if err != nil {
		return err
	}

	// Get issue (open or closed)
	ref, err := cfg.store.FindRef(id)
	if err != nil {
		return fmt.Errorf("issue not found: %s", issuelib.FormatID(id))
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
	issueIDStr := issuelib.FormatID(id)
	_ = cfg.store.AddNote(commitSHA, issueIDStr)

	fmt.Fprintf(cc.Out, "Linked issue #%s to commit %s\n", issueIDStr, commitSHA[:7])
	return nil
}
