package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
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
	report, err := ops.Pull(cfg.store, remote, cfg.Force, cfg.DryRun)
	if err != nil {
		return err
	}
	if report.Cleaned > 0 {
		fmt.Fprintf(cc.Out, "Cleaned up %d stale ref(s).\n", report.Cleaned)
	}
	if err := writeReport(cc, report); err != nil {
		return err
	}
	fmt.Fprintln(cc.Out, "Done.")
	return nil
}
