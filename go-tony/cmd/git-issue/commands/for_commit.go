package commands

import (
	"fmt"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
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

	commitSHA, err := cfg.store.VerifyCommit(args[0])
	if err != nil {
		return err
	}

	// Get git notes for this commit
	notes, err := cfg.store.GetNotes(commitSHA)
	if err != nil {
		fmt.Fprintf(cc.Out, "No issues linked to commit %s\n", commitSHA[:7])
		return nil
	}

	// Parse issue IDs from notes
	issueIDs := strings.Split(notes, "\n")
	if len(issueIDs) == 0 || (len(issueIDs) == 1 && issueIDs[0] == "") {
		fmt.Fprintf(cc.Out, "No issues linked to commit %s\n", commitSHA[:7])
		return nil
	}

	// Get commit info
	commitInfo, _ := cfg.store.GetCommitInfo(commitSHA)
	fmt.Fprintf(cc.Out, "Issues linked to commit %s:\n\n", commitInfo)

	// Show each linked issue
	for _, idStr := range issueIDs {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}

		id, err := issuelib.ParseID(idStr)
		if err != nil {
			fmt.Fprintf(cc.Out, "  #%s (invalid)\n", idStr)
			continue
		}

		ref, err := cfg.store.FindRef(id)
		if err != nil {
			fmt.Fprintf(cc.Out, "  #%s (not found)\n", idStr)
			continue
		}

		issue, _, err := cfg.store.GetByRef(ref)
		if err != nil {
			fmt.Fprintf(cc.Out, "  #%s (error)\n", idStr)
			continue
		}

		status := issuelib.StatusFromRef(ref)
		fmt.Fprintf(cc.Out, "  #%s %s[%s]%s %s\n",
			idStr,
			issuelib.StatusColor(status),
			status,
			issuelib.ColorReset,
			issue.Title,
		)
	}

	return nil
}
