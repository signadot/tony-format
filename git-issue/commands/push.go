package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type pushConfig struct {
	*cli.Command
	store  issuelib.Store
	All    bool `cli:"name=all desc='Push all issues'"`
	Force  bool `cli:"name=force desc='where an issue was edited on both sides, take this clone'"`
	DryRun bool `cli:"name=dry-run aliases=n desc='say what a push would do, and write nothing'"`
}

// newPushConfig builds the command and the config behind it, as pull does.
func newPushConfig(store issuelib.Store) *pushConfig {
	cfg := &pushConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	cli.NewCommandAt(&cfg.Command, "push").
		WithSynopsis("push [--force] [--dry-run] [<id>] [remote] - Push an issue, or every issue, to the remote").
		WithOpts(opts...).
		WithRun(cfg.run)
	return cfg
}

// PushCommand returns the push subcommand.
func PushCommand(store issuelib.Store) *cli.Command {
	return newPushConfig(store).Command
}

func (cfg *pushConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	// Get remote name (default to origin)
	remote := "origin"

	// No id is every issue, as a pull is the whole repository; --all says the
	// same and takes the remote as its argument.
	if cfg.All || len(args) == 0 {
		if len(args) > 0 {
			remote = args[0]
		}
		return cfg.pushAll(cc, remote)
	}

	xidrOrPrefix := args[0]

	if len(args) > 1 {
		remote = args[1]
	}

	return cfg.pushSingle(cc, remote, xidrOrPrefix)
}

func (cfg *pushConfig) pushAll(cc *cli.Context, remote string) error {
	if err := cfg.store.VerifyRemote(remote); err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "Pushing all issues to %s...\n", remote)
	return cfg.push(cc, remote, "")
}

func (cfg *pushConfig) pushSingle(cc *cli.Context, remote string, xidrOrPrefix string) error {
	if err := cfg.store.VerifyRemote(remote); err != nil {
		return err
	}
	ref, err := cfg.store.FindRef(xidrOrPrefix)
	if err != nil {
		return err
	}
	xidr, err := issuelib.XIDRFromRef(ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "Pushing issue %s to %s...\n", issuelib.FormatID(xidr), remote)
	return cfg.push(cc, remote, xidr)
}

// push syncs one issue to the remote, or every issue when xidr is empty, and
// writes the report.
func (cfg *pushConfig) push(cc *cli.Context, remote, xidr string) error {
	report, err := ops.Push(cfg.store, remote, xidr, cfg.Force, cfg.DryRun)
	if err != nil {
		return err
	}
	if err := writeReport(cc, report); err != nil {
		return err
	}
	fmt.Fprintln(cc.Out, "Done.")
	return nil
}
