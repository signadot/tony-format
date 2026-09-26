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
//
// It pulls origin every -fetch, when there is an origin, and says what each
// pull did (watch_pull.go): issues often travel by push and pull, and a watch
// that only read this clone would hear nothing pushed. --remote names another
// remote; --local watches this clone alone.

type watchConfig struct {
	*cli.Command
	store  issuelib.Store
	Label  string `cli:"name=label aliases=l desc='only issues carrying this label before or after the change'"`
	Remote string `cli:"name=remote desc='the remote to pull (default origin, when there is one)'"`
	Local  bool   `cli:"name=local desc='pull nothing; watch this clone alone'"`
	Poll   time.Duration
	Fetch  time.Duration
}

// WatchCommand returns the watch subcommand.
func WatchCommand(store issuelib.Store) *cli.Command {
	cfg := &watchConfig{store: store, Poll: watchInterval, Fetch: defaultFetch}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "watch").
		WithSynopsis("watch [--label <label>] [--remote <name> | --local] [-poll <duration>] [-fetch <duration>] [<id>...] - Print each issue that changes, as it changes, until stopped").
		WithOpts(append(opts, pollOpt(&cfg.Poll, ""), fetchOpt(&cfg.Fetch))...).
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
	var p *puller
	switch {
	case cfg.Local && cfg.Remote != "":
		return fmt.Errorf("%w: --local and --remote: one or the other", cli.ErrUsage)
	case cfg.Remote != "":
		if err := cfg.store.VerifyRemote(cfg.Remote); err != nil {
			return err
		}
		p = newPuller(cfg.store, "", cfg.Remote)
	case !cfg.Local:
		if cfg.store.VerifyRemote("origin") == nil {
			p = newPuller(cfg.store, "", "origin")
		} else {
			fmt.Fprintln(cc.Err, "no origin: watching this clone alone")
		}
	}
	ctx := cc.Go
	if ctx == nil {
		ctx = context.Background()
	}
	return watchStore(ctx, cfg.store, f, cfg.Poll, p, cfg.Fetch,
		func(ch watchChange) { fmt.Fprintln(cc.Out, ch.oneLine()) },
		func(n pullNote) { fmt.Fprintln(cc.Out, n.line()) })
}

// watchStore looks at a repository's issue refs every interval until ctx
// ends, and hands each change f matches to emit. With a puller it pulls every
// fetch, and hands to note what stands -- a refusal, a failure -- when it
// begins and when it clears, and what the pull did to an issue just before
// the change it brought. It pulls once before its first look, so what that
// brings is where the watch begins.
func watchStore(ctx context.Context, st issuelib.Store, f watchFilter, interval time.Duration, p *puller, fetch time.Duration, emit func(watchChange), note func(pullNote)) error {
	wants := func(xidr string) bool { return f.wants("", xidr) }
	said := alarmsSaid{}
	var did []pullNote
	pull := func() {
		d, _ := p.pull()
		did = append(did, d...)
		for _, n := range said.news(p.stands(wants), p.remote) {
			note(n)
		}
	}
	var fetchC <-chan time.Time
	if p != nil {
		pull()
		did = nil
		t := time.NewTicker(fetch)
		defer t.Stop()
		fetchC = t.C
	}
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
		case <-fetchC:
			pull()
		}
		// A look that fails -- git busy, a ref mid-update -- is skipped, not
		// fatal: the next one compares with the last that succeeded.
		now, err := lookAt(st)
		if err != nil {
			continue
		}
		for _, xidr := range moved(was, now) {
			if !wants(xidr) {
				continue
			}
			if ch, ok := describe(st, "", xidr, was[xidr], now[xidr], began); ok && f.matches(ch) {
				for _, n := range withChanges(did, []watchChange{ch}) {
					note(n)
				}
				emit(ch)
			}
		}
		was, did = now, nil
	}
}
