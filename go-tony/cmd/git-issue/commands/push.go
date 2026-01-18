package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type pushConfig struct {
	*cli.Command
	store issuelib.Store
	All   bool `cli:"name=all desc='Push all issues'"`
}

// PushCommand returns the push subcommand.
func PushCommand(store issuelib.Store) *cli.Command {
	cfg := &pushConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "push").
		WithSynopsis("push [--all] <id> [remote] - Push issue(s) to remote").
		WithOpts(opts...).
		WithRun(cfg.run)
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

	refspecs := []string{
		"+refs/issues/*:refs/issues/*",
		"+refs/closed/*:refs/closed/*",
		"+refs/meta/issue-counter:refs/meta/issue-counter",
		"+refs/notes/issues:refs/notes/issues",
	}

	if err := cfg.store.Push(remote, refspecs); err != nil {
		return err
	}

	fmt.Fprintln(cc.Out, "Done.")
	return nil
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

	// Get issue for display
	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Pushing issue %s to %s...\n", issuelib.FormatID(issue.ID), remote)

	// Push the issue ref
	refspecs := []string{fmt.Sprintf("+%s:%s", ref, ref)}
	if err := cfg.store.Push(remote, refspecs); err != nil {
		return fmt.Errorf("failed to push issue: %w", err)
	}

	// Push notes for commits referenced by this issue
	if len(issue.Commits) > 0 {
		_ = cfg.store.Push(remote, []string{"+refs/notes/issues:refs/notes/issues"})
	}

	fmt.Fprintf(cc.Out, "Pushed issue %s\n", issuelib.FormatID(issue.ID))
	return nil
}
