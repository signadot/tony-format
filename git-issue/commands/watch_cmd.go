package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

// `git issue watch` prints a line for each issue that changes, as it changes,
// until it is stopped. It is the watch for an agent whose host wakes it on a
// background command's output -- Claude Code's Monitor, say -- where an MCP
// tool call would hold the agent's turn (vegmw7bmh12ks11mq1n0). It runs until
// it is stopped, so no change is missed between two runs of it.

type watchConfig struct {
	*cli.Command
	store issuelib.Store
	Label string `cli:"name=label aliases=l desc='only issues carrying this label before or after the change'"`
	Poll  time.Duration
}

// WatchCommand returns the watch subcommand.
func WatchCommand(store issuelib.Store) *cli.Command {
	cfg := &watchConfig{store: store, Poll: watchInterval}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "watch").
		WithSynopsis("watch [--label <label>] [-poll <duration>] [<id>...] - Print each issue that changes, as it changes, until stopped").
		WithOpts(append(opts, pollOpt(&cfg.Poll, ""))...).
		WithRun(cfg.run)
}

func (cfg *watchConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}
	if err := cfg.store.VerifyRepository(); err != nil {
		return err
	}
	var f watchFilter
	if cfg.Label != "" {
		f.label = issuelib.NormalizeLabel(cfg.Label)
	}
	if len(args) > 0 {
		f.ids = map[string]bool{}
		for _, id := range args {
			ref, err := cfg.store.FindRef(id)
			if err != nil {
				return err
			}
			xidr, err := issuelib.XIDRFromRef(ref)
			if err != nil {
				return err
			}
			f.ids[xidr] = true
		}
	}
	ctx := cc.Go
	if ctx == nil {
		ctx = context.Background()
	}
	return watchStore(ctx, cfg.store, f, cfg.Poll, func(ch watchChange) {
		fmt.Fprintln(cc.Out, ch.oneLine())
	})
}

// watchStore looks at a repository's issue refs every interval until ctx ends,
// and hands each change f matches to emit.
func watchStore(ctx context.Context, st issuelib.Store, f watchFilter, interval time.Duration, emit func(watchChange)) error {
	began := time.Now()
	was, err := lookAt(st)
	if err != nil {
		return err
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		// A look that fails -- git busy, a ref mid-update -- is skipped, not
		// fatal: the next one compares with the last that succeeded.
		now, err := lookAt(st)
		if err != nil {
			continue
		}
		for _, xidr := range moved(was, now) {
			if !f.wants("", xidr) {
				continue
			}
			if ch, ok := describe(st, "", xidr, was[xidr], now[xidr], began); ok && f.matches(ch) {
				emit(ch)
			}
		}
		was = now
	}
}
