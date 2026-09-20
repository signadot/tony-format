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
		mirror(issuelib.OpenPrefix + "*"),
		mirror(issuelib.ClosedPrefix + "*"),
		mirror(issuelib.NotesRef),
		// A remote nothing of this generation has pushed to still keeps its
		// issues where the older layout put them. They are fetched here and
		// adopted below, so a client that has upgraded sees them either way.
		mirror(issuelib.Gen0OpenPrefix + "*"),
		mirror(issuelib.Gen0ClosedPrefix + "*"),
		mirror(issuelib.Gen0NotesRef),
	}

	if err := cfg.store.Fetch(remote, refspecs); err != nil {
		return err
	}

	// Explicitly, and not through the once a read would take: refs arrived a
	// moment ago, and the once may have been spent before they did.
	if err := cfg.store.AdoptGen0(); err != nil {
		return err
	}

	// Clean up stale refs (when an issue exists in both namespaces)
	// Keeps the ref with more history (the descendant)
	cleaned, _ := cfg.store.CleanupStaleRefs()
	if cleaned > 0 {
		fmt.Fprintf(cc.Out, "Cleaned up %d stale ref(s).\n", cleaned)
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

// mirror is the refspec that copies a ref, or every ref under a pattern, to the
// same name on the other side, overwriting what is there.
func mirror(pattern string) string {
	return "+" + pattern + ":" + pattern
}
