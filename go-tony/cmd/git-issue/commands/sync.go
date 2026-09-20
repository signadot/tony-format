package commands

import (
	"errors"
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// What a push and a pull have in common: they decide from the same plans, report
// what they did in the same shape, and refuse the same thing.
//
// A sync that refused or failed anything exits non-zero. An operator reads the
// lines; a script reads the status, and must not have to parse prose to learn
// that an issue was left behind.

// refusal is an issue a sync left alone, and why: the two sides hold work that
// cannot be brought together, and a person decides.
type refusal struct {
	plan   issuelib.IssuePlan
	reason string
}

// syncReport gathers one direction of a sync as it runs, and writes it out.
type syncReport struct {
	remote  string
	pulling bool
	force   bool
	dryRun  bool

	// changed holds a line per issue something happened to. An issue a sync
	// leaves alone says nothing: most of them, most of the time.
	changed   []string
	unchanged int
	refused   []refusal
	oldClient []issuelib.IssuePlan
	failed    []error
}

// run carries out one direction over the plans. act writes for one issue and
// says what it did; a plan it would have to force is refused instead, and one
// that fails is recorded and does not stop the rest -- one unsyncable issue
// should not strand the others.
func (r *syncReport) run(store issuelib.Store, plans []issuelib.IssuePlan,
	act func(issuelib.IssuePlan) (string, error)) {
	for _, p := range plans {
		if p.OldClient {
			r.oldClient = append(r.oldClient, p)
		}
		if r.dryRun {
			if what := r.intent(p); what != "" {
				r.changed = append(r.changed, fmt.Sprintf("%s  %s (%s)",
					issuelib.FormatID(p.XIDR), what, p.Verdict))
			} else {
				r.unchanged++
			}
			continue
		}
		did, err := act(p)
		switch {
		case errors.Is(err, issuelib.ErrDiverged):
			r.refused = append(r.refused, refusal{plan: p, reason: err.Error()})
		case err != nil:
			r.failed = append(r.failed, err)
		case did == "":
			r.unchanged++
		default:
			r.changed = append(r.changed, fmt.Sprintf("%s  %s", issuelib.FormatID(p.XIDR), did))
		}
	}
}

// intent is what this direction would do about the plan, for a dry run: the
// shape of the answer, without the commits a real run would report.
func (r *syncReport) intent(p issuelib.IssuePlan) string {
	if p.Verdict == issuelib.Diverged {
		if r.force {
			if r.pulling {
				return "take the remote side"
			}
			return "take this clone"
		}
		return "merge the two sides"
	}
	if r.pulling {
		switch p.Verdict {
		case issuelib.RemoteOnly:
			return "create here"
		case issuelib.Behind:
			return "bring forward"
		case issuelib.Equal:
			if p.Local != nil && p.R != nil && p.Local.Closed != p.R.Closed {
				return "take the remote's status"
			}
		}
		return ""
	}
	switch p.Verdict {
	case issuelib.LocalOnly, issuelib.Ahead, issuelib.Equal:
		if p.Local == nil {
			return ""
		}
		want := issuelib.RefForXIDR(p.XIDR)
		if p.Local.Closed {
			want = issuelib.ClosedRefForXIDR(p.XIDR)
		}
		for _, tip := range p.Remote {
			if tip.Ref == want && tip.Commit == p.Local.Commit && len(p.Remote) == 1 {
				return ""
			}
		}
		return "send to the remote"
	}
	return ""
}

// write reports the run and answers whether it was whole. What was refused or
// failed is named, because the point of refusing is that a person decides.
func (r *syncReport) write(cc *cli.Context, store issuelib.Store) error {
	for _, line := range r.changed {
		fmt.Fprintf(cc.Out, "  %s\n", line)
	}

	verb := "changed"
	if r.dryRun {
		verb = "to change"
	}
	fmt.Fprintf(cc.Out, "%d issue(s) %s, %d unchanged", len(r.changed), verb, r.unchanged)
	if len(r.refused) > 0 {
		fmt.Fprintf(cc.Out, ", %d refused", len(r.refused))
	}
	if len(r.failed) > 0 {
		fmt.Fprintf(cc.Out, ", %d failed", len(r.failed))
	}
	fmt.Fprintln(cc.Out, ".")

	for _, ref := range r.refused {
		r.writeRefusal(cc, store, ref)
	}
	for _, err := range r.failed {
		fmt.Fprintf(cc.Err, "  %v\n", err)
	}
	if len(r.oldClient) > 0 {
		r.writeOldClient(cc)
	}

	switch {
	case len(r.failed) > 0:
		return errors.Join(r.failed...)
	case len(r.refused) > 0:
		return fmt.Errorf("%d issue(s) could not be brought together and were left alone", len(r.refused))
	}
	return nil
}

// writeRefusal names one issue nobody can settle automatically, and says what
// saying so again would do.
func (r *syncReport) writeRefusal(cc *cli.Context, store issuelib.Store, ref refusal) {
	p := ref.plan
	title := ""
	if p.Local != nil {
		if issue, _, err := store.GetByRef(p.Local.Ref); err == nil {
			title = "  " + issue.Title
		}
	}
	fmt.Fprintf(cc.Out, "  %s%s\n", issuelib.FormatID(p.XIDR), title)
	fmt.Fprintf(cc.Out, "      %s.\n", ref.reason)

	here, there := "nothing", "nothing"
	if p.Local != nil {
		here = shortSHA(p.Local.Commit)
	}
	if p.Split {
		fmt.Fprintf(cc.Out, "      %s holds this issue at two tips that disagree, which a "+
			"git-issue older than this one can cause.\n", r.remote)
	} else {
		if p.R != nil {
			there = shortSHA(p.R.Commit)
		}
		fmt.Fprintf(cc.Out, "      here %s, %s %s.\n", here, r.remote, there)
	}
	takes, other := "the remote's", "this clone's"
	command := "pull"
	if !r.pulling {
		takes, other = "this clone's", "the remote's"
		command = "push"
	}
	fmt.Fprintf(cc.Out, "      `git issue %s --force` takes %s; %s stays in the ref's reflog.\n",
		command, takes, other)
}

// writeOldClient says that someone is running a git-issue too old to see the
// current refs. Nothing of theirs is lost -- their work is adopted and the next
// push clears where they put it -- but it will keep happening until they
// upgrade, and the people who can tell them are the ones reading this.
func (r *syncReport) writeOldClient(cc *cli.Context) {
	var ids []string
	for _, p := range r.oldClient {
		ids = append(ids, issuelib.FormatID(p.XIDR))
	}
	fmt.Fprintf(cc.Out, "\nA git-issue older than this one has pushed %d issue(s) to %s: %s.\n",
		len(ids), r.remote, join(ids))
	fmt.Fprintf(cc.Out, "Nothing was lost. Whoever pushed them should upgrade.\n")
}

// join lists ids for a person, and stops before a line of them.
func join(ids []string) string {
	const most = 6
	out := ""
	for i, id := range ids {
		if i == most {
			return fmt.Sprintf("%s and %d more", out, len(ids)-most)
		}
		if i > 0 {
			out += ", "
		}
		out += id
	}
	return out
}

// shortSHA abbreviates a commit for a message to a person.
func shortSHA(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
