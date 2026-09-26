package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

type pullConfig struct {
	*cli.Command
	store  issuelib.Store
	Force  bool `cli:"name=force desc='where an issue was edited on both sides, take the remote side'"`
	DryRun bool `cli:"name=dry-run aliases=n desc='say what a pull would do, and write nothing'"`
}

// newPullConfig builds the command and the config behind it. The two are made
// together because run parses its own flags, so a config without its command
// cannot run -- which is what a test builds as well.
func newPullConfig(store issuelib.Store) *pullConfig {
	cfg := &pullConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	cli.NewCommandAt(&cfg.Command, "pull").
		WithSynopsis("pull [--force] [--dry-run] [remote] - Pull issues from remote").
		WithOpts(opts...).
		WithRun(cfg.run)
	return cfg
}

// PullCommand returns the pull subcommand.
func PullCommand(store issuelib.Store) *cli.Command {
	return newPullConfig(store).Command
}

func (cfg *pullConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	// Get remote name (default to origin)
	remote := "origin"
	if len(args) > 0 {
		remote = args[0]
	}

	if err := cfg.store.VerifyRemote(remote); err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Fetching issues from %s...\n", remote)
	if _, err := cfg.store.FetchTracking(remote); err != nil {
		return err
	}
	plans, err := cfg.store.PlanSync(remote)
	if err != nil {
		return err
	}

	report := &syncReport{remote: remote, pulling: true, force: cfg.Force, dryRun: cfg.DryRun}
	report.run(cfg.store, plans, func(p issuelib.IssuePlan) (string, error) {
		return cfg.store.ApplyPull(p, cfg.Force)
	})

	if !cfg.DryRun {
		if err := cfg.store.SyncNotes(remote, false); err != nil {
			return err
		}
		// A pull no longer leaves an issue in both namespaces -- it decides
		// which one each issue is in before writing -- but a repository that
		// already held such a pair is still put right.
		if cleaned, _ := cfg.store.CleanupStaleRefs(); cleaned > 0 {
			fmt.Fprintf(cc.Out, "Cleaned up %d stale ref(s).\n", cleaned)
		}
	}

	if err := report.write(cc, cfg.store); err != nil {
		return err
	}
	fmt.Fprintln(cc.Out, "Done.")
	return nil
}
