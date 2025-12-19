package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type pullConfig struct {
	*cli.Command
	store issuelib.Store
}

// PullCommand returns the pull subcommand.
func PullCommand(store issuelib.Store) *cli.Command {
	cfg := &pullConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "pull").
		WithSynopsis("pull [remote] - Pull issues from remote").
		WithRun(cfg.run)
}

func (cfg *pullConfig) run(cc *cli.Context, args []string) error {
	// Get remote name (default to origin)
	remote := "origin"
	if len(args) > 0 {
		remote = args[0]
	}

	if err := cfg.store.VerifyRemote(remote); err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Fetching issues from %s...\n", remote)

	refspecs := []string{
		"+refs/issues/*:refs/issues/*",
		"+refs/closed/*:refs/closed/*",
		"+refs/meta/issue-counter:refs/meta/issue-counter",
		"+refs/notes/issues:refs/notes/issues",
	}

	if err := cfg.store.Fetch(remote, refspecs); err != nil {
		return err
	}

	// Count how many issues we have now
	refs, err := cfg.store.ListRefs(true)
	if err != nil {
		fmt.Fprintln(cc.Out, "Done.")
		return nil
	}

	fmt.Fprintf(cc.Out, "Done. %d issue(s) in local repository.\n", len(refs))
	return nil
}
