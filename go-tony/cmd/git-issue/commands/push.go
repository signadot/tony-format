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

	localRefs, err := cfg.store.ListRefs(true)
	if err != nil {
		return err
	}
	stale, err := cfg.staleRemoteRefs(remote, localRefs)
	if err != nil {
		return err
	}
	refspecs = append(refspecs, deletions(stale)...)

	if err := cfg.store.Push(remote, refspecs); err != nil {
		return err
	}

	if len(stale) > 0 {
		fmt.Fprintf(cc.Out, "Deleted %d moved-from ref(s) on %s.\n", len(stale), remote)
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

	// Ask what the remote holds before touching it: a push that only adds the
	// ref the issue is at now leaves the one it used to be at, and the remote
	// holds the issue open and closed at once.
	stale, err := cfg.staleRemoteRefs(remote, []string{ref})
	if err != nil {
		return err
	}

	// Push the issue ref, then delete the namespace it moved out of.
	refspecs := append([]string{fmt.Sprintf("+%s:%s", ref, ref)}, deletions(stale)...)
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

// staleRemoteRefs returns the refs the remote holds for issues localRefs names,
// in the status namespace those issues are NOT in locally. Closing an issue
// moves its ref, and a push that does not mirror the move leaves the remote with
// an id in both refs/issues/ and refs/closed/ -- a state no repository ever has
// locally, and one no consumer can read a status out of, since a close and a
// reopen leave the same pair of refs behind.
//
// Only issues held locally are considered, so an issue the remote has and this
// repository has never fetched is left alone; pushing is not pruning.
func (cfg *pushConfig) staleRemoteRefs(remote string, localRefs []string) ([]string, error) {
	movedFrom := make(map[string]bool, len(localRefs))
	for _, ref := range localRefs {
		if other, err := counterpartRef(ref); err == nil {
			movedFrom[other] = true
		}
	}
	if len(movedFrom) == 0 {
		return nil, nil
	}

	remoteRefs, err := cfg.store.RemoteRefs(remote, "refs/issues/*", "refs/closed/*")
	if err != nil {
		return nil, err
	}

	var stale []string
	for _, ref := range remoteRefs {
		if movedFrom[ref] {
			stale = append(stale, ref)
		}
	}
	return stale, nil
}

// counterpartRef returns the ref the issue at ref would have in the other status
// namespace.
func counterpartRef(ref string) (string, error) {
	xidr, err := issuelib.XIDRFromRef(ref)
	if err != nil {
		return "", err
	}
	if issuelib.IsClosedRef(ref) {
		return issuelib.RefForXIDR(xidr), nil
	}
	return issuelib.ClosedRefForXIDR(xidr), nil
}

// deletions turns refs into the refspecs that delete them on a remote.
func deletions(refs []string) []string {
	specs := make([]string, 0, len(refs))
	for _, ref := range refs {
		specs = append(specs, ":"+ref)
	}
	return specs
}
