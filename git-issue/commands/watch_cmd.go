package commands

import (
	"context"
	"fmt"
	"strings"
	"sync"
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
	// Every repository the command covers: the one it runs on, or outside a
	// repository the set, each line then saying which.
	repos := targets(cfg.store)
	type watched struct {
		r *repo
		f watchFilter
		p *puller
	}
	var watches []watched
	found := map[string]bool{}
	for _, r := range repos {
		if err := r.Store.VerifyRepository(); err != nil {
			return err
		}
		var f watchFilter
		if cfg.Label != "" {
			f.label = issuelib.NormalizeLabel(cfg.Label)
		}
		// An issue named is watched in the repository that holds it, and a
		// repository that holds none of those named is not watched.
		if len(args) > 0 {
			f.ids = map[string]bool{}
			for _, id := range args {
				ref, err := r.Store.FindRef(id)
				if err != nil {
					if len(repos) > 1 && !strings.Contains(err.Error(), "ambiguous") {
						continue
					}
					return err
				}
				xidr, err := issuelib.XIDRFromRef(ref)
				if err != nil {
					return err
				}
				f.ids[xidr], found[id] = true, true
			}
			if len(f.ids) == 0 {
				continue
			}
		}
		name := ""
		if len(repos) > 1 {
			name = r.Name
		}
		p, err := watchPuller(cc, r.Store, name, cfg.Remote, cfg.Local)
		if err != nil {
			return err
		}
		watches = append(watches, watched{r, f, p})
	}
	for _, id := range args {
		if !found[id] {
			return fmt.Errorf("issue not found: %s", id)
		}
	}
	ctx := cc.Go
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// One watch a repository, saying what they find a line at a time.
	var mu sync.Mutex
	say := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintln(cc.Out, line)
	}
	errs := make(chan error, len(watches))
	for _, w := range watches {
		name := ""
		if len(repos) > 1 {
			name = w.r.Name
		}
		go func() {
			errs <- watchStore(ctx, w.r.Store, w.f, cfg.Poll, w.p, cfg.Fetch,
				func(ch watchChange) { ch.Repo = name; say(ch.oneLine()) },
				func(n pullNote) { say(n.line()) }, nil)
		}()
	}
	// A watch that ends with an error ends them all; one ended with ctx ends
	// with nil, as the rest do.
	for range watches {
		if err := <-errs; err != nil {
			return err
		}
	}
	return nil
}

// watchStore looks at a repository's issue refs every interval until ctx
// ends, and hands each change f matches to emit. With a puller it pulls every
// fetch, and hands to note what stands -- a refusal, a failure -- when it
// begins and when it clears, and what the pull did to an issue just before
// the change it brought. It pulls once before its first look, so what that
// brings is where the watch begins. It looks between pulls, not during one,
// so a change made here while it pulls is said when the pull is done. pulled,
// when there is one, is called after each pull.
func watchStore(ctx context.Context, st issuelib.Store, f watchFilter, interval time.Duration, p *puller, fetch time.Duration, emit func(watchChange), note func(pullNote), pulled func()) error {
	wants := func(xidr string) bool { return f.wants("", xidr) }
	scoped := func(xidr string) bool { return f.wantsIssue(st, "", xidr) }
	said := alarmsSaid{}
	var did []pullNote
	pull := func() {
		d, _ := p.pull(0)
		did = append(did, d...)
		for _, n := range said.news(p.stands(scoped), p.remote) {
			note(n)
		}
		if pulled != nil {
			pulled()
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
	remote := watchRemote(st, p)
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
			if ch, ok := describe(st, "", remote, xidr, was[xidr], now[xidr], began); ok && f.matches(ch) {
				for _, n := range withChanges(did, []watchChange{ch}) {
					note(n)
				}
				emit(ch)
			}
		}
		was, did = now, nil
	}
}

// watchRemote is the remote a watch's changes say they are on or not: the one
// it pulls, or origin when it pulls none; none without an origin.
func watchRemote(st issuelib.Store, p *puller) string {
	switch {
	case p != nil:
		return p.remote
	case st.VerifyRemote("origin") == nil:
		return "origin"
	}
	return ""
}
