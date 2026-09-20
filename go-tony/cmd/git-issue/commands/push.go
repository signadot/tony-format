package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
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
		WithSynopsis("push [--all] [--force] [--dry-run] <id> [remote] - Push issue(s) to remote").
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

	if cfg.All {
		if len(args) > 0 {
			remote = args[0]
		}
		return cfg.pushAll(cc, remote)
	}

	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue push <xidr> [remote]", cli.ErrUsage)
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

	// Find the issue ref (open or closed)
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

// push syncs one issue to the remote, or every issue when xidr is empty.
//
// Both forms ask the remote what it holds first and decide from that, so what a
// push writes is settled before anything is sent: an issue whose remote ref this
// clone's tip does not carry is left alone and reported, rather than overwritten
// because a refspec said so.
func (cfg *pushConfig) push(cc *cli.Context, remote, xidr string) error {
	if _, err := cfg.store.FetchTracking(remote); err != nil {
		return err
	}
	plans, err := cfg.store.PlanSync(remote)
	if err != nil {
		return err
	}
	if xidr != "" {
		plans = plansFor(plans, xidr)
	}

	report := &syncReport{remote: remote, force: cfg.Force, dryRun: cfg.DryRun}
	report.run(cfg.store, plans, func(p issuelib.IssuePlan) (string, error) {
		return cfg.store.ApplyPush(remote, p, cfg.Force)
	})

	if !cfg.DryRun {
		if err := cfg.store.SyncNotes(remote, true); err != nil {
			return err
		}
	}

	if err := report.write(cc, cfg.store); err != nil {
		return err
	}
	fmt.Fprintln(cc.Out, "Done.")
	return nil
}

// plansFor narrows a plan of the whole repository to one issue.
func plansFor(plans []issuelib.IssuePlan, xidr string) []issuelib.IssuePlan {
	for _, p := range plans {
		if p.XIDR == xidr {
			return []issuelib.IssuePlan{p}
		}
	}
	return nil
}
