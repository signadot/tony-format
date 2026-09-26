package ops

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// A push and a pull decide from the same plans, report what they did in the same
// shape, and refuse the same thing. An issue whose two sides hold work that cannot
// be brought together is left alone and named, because the point of refusing is
// that a person decides; one unsyncable issue does not strand the others.

// Change is one issue a sync did something to.
type Change struct {
	ID   string
	What string // what was done, or in a dry run what would be
}

// Refusal is one issue a sync left alone, and why.
type Refusal struct {
	ID     string
	Title  string
	Reason string
	Here   string // this clone's commit, abbreviated, or "nothing"
	There  string // the remote's, or "nothing"
	Split  bool   // the remote holds the issue at two tips that disagree
}

// Report is one direction of a sync, as it went.
type Report struct {
	Remote  string
	Pulling bool
	Force   bool
	DryRun  bool

	// Changed holds an entry per issue something happened to. An issue a sync
	// leaves alone says nothing: most of them, most of the time.
	Changed   []Change
	Unchanged int
	Refused   []Refusal
	OldClient []string // issues a git-issue older than this one pushed
	Failed    []error
	Cleaned   int // stale refs a pull removed

	// Mirrors: what a pull refreshed from each source, and the sources it could
	// not reach, whose mirrors stay as they were. An unreached source is said,
	// not failed: the pull did what the remote holds.
	Refreshed map[string]int
	Unreached map[string]string
}

// Err is whether the run was whole: nil when nothing was refused or failed.
func (r *Report) Err() error {
	switch {
	case len(r.Failed) > 0:
		return errors.Join(r.Failed...)
	case len(r.Refused) > 0:
		return fmt.Errorf("%d issue(s) could not be brought together and were left alone", len(r.Refused))
	}
	return nil
}

// Push sends one issue, or every issue when id is empty, to the remote. What a
// push writes is settled before anything is sent: an issue whose remote ref this
// clone's tip does not carry is left alone and reported, rather than overwritten
// because a refspec said so.
func Push(s issuelib.Store, remote, id string, force, dryRun bool) (*Report, error) {
	if remote == "" {
		remote = "origin"
	}
	if err := s.VerifyRemote(remote); err != nil {
		return nil, err
	}
	xidr := ""
	if id != "" {
		ref, err := s.FindRef(id)
		if err != nil {
			return nil, err
		}
		if xidr, err = issuelib.XIDRFromRef(ref); err != nil {
			return nil, err
		}
	}
	if _, err := s.FetchTracking(remote); err != nil {
		return nil, err
	}
	plans, err := s.PlanSync(remote)
	if err != nil {
		return nil, err
	}
	if xidr != "" {
		plans = plansFor(plans, xidr)
	}
	// The pushes go first and together, since a connection per issue is what
	// made a push of a repository take minutes; the report then reads what each
	// issue came to.
	done := map[string]issuelib.PushResult{}
	if !dryRun {
		for i, r := range s.ApplyPushes(remote, plans, force) {
			done[plans[i].XIDR] = r
		}
	}
	report := &Report{Remote: remote, Force: force, DryRun: dryRun}
	report.run(s, plans, func(p issuelib.IssuePlan) (string, error) {
		r := done[p.XIDR]
		return r.Did, r.Err
	})
	// The mirrors and sources go with the issues, to this remote and nowhere
	// else; a push of one issue carries them all, since a relation from it may
	// name any of them.
	carried, err := s.PlanCarried(remote)
	if err != nil {
		return nil, err
	}
	cdone := map[string]issuelib.PushResult{}
	if !dryRun {
		for i, r := range s.ApplyCarriedPushes(remote, carried, force) {
			cdone[carried[i].Ref] = r
		}
	}
	report.runCarried(carried, func(p issuelib.CarriedPlan) (string, error) {
		r := cdone[p.Ref]
		return r.Did, r.Err
	})
	if !dryRun {
		if err := s.SyncNotes(remote, true); err != nil {
			return nil, err
		}
	}
	return report, nil
}

// Pull brings what the remote holds into this clone.
func Pull(s issuelib.Store, remote string, force, dryRun bool) (*Report, error) {
	if remote == "" {
		remote = "origin"
	}
	if err := s.VerifyRemote(remote); err != nil {
		return nil, err
	}
	if _, err := s.FetchTracking(remote); err != nil {
		return nil, err
	}
	plans, err := s.PlanSync(remote)
	if err != nil {
		return nil, err
	}
	report := &Report{Remote: remote, Pulling: true, Force: force, DryRun: dryRun}
	report.run(s, plans, func(p issuelib.IssuePlan) (string, error) {
		return s.ApplyPull(p, force)
	})
	carried, err := s.PlanCarried(remote)
	if err != nil {
		return nil, err
	}
	report.runCarried(carried, func(p issuelib.CarriedPlan) (string, error) {
		if dryRun {
			return "", nil
		}
		return s.ApplyCarriedPull(p, force)
	})
	if !dryRun {
		if err := s.SyncNotes(remote, false); err != nil {
			return nil, err
		}
		// A pull no longer leaves an issue in both namespaces -- it decides
		// which one each issue is in before writing -- but a repository that
		// already held such a pair is still put right.
		report.Cleaned, _ = s.CleanupStaleRefs()
		// Then the mirrors follow their sources. A source that cannot be
		// reached leaves its mirrors as they were, and is named.
		sources, err := s.Sources()
		if err != nil {
			return nil, err
		}
		for _, src := range sources {
			n, err := s.RefreshMirrors(src.Name)
			if err != nil {
				if report.Unreached == nil {
					report.Unreached = map[string]string{}
				}
				report.Unreached[src.Name] = err.Error()
				continue
			}
			if report.Refreshed == nil {
				report.Refreshed = map[string]int{}
			}
			report.Refreshed[src.Name] = n
		}
	}
	return report, nil
}

// runCarried carries out one direction over the carried refs, as run does over
// the issues, and records each under its name.
func (r *Report) runCarried(plans []issuelib.CarriedPlan, act func(issuelib.CarriedPlan) (string, error)) {
	for _, p := range plans {
		if r.DryRun {
			if what := r.carriedIntent(p); what != "" {
				r.Changed = append(r.Changed, Change{ID: p.Name(), What: fmt.Sprintf("%s (%s)", what, p.Verdict)})
			} else {
				r.Unchanged++
			}
			continue
		}
		did, err := act(p)
		switch {
		case errors.Is(err, issuelib.ErrDiverged):
			r.Refused = append(r.Refused, Refusal{ID: p.Name(), Reason: err.Error(),
				Here: shortOrNothing(p.Local), There: shortOrNothing(p.Remote)})
		case err != nil:
			r.Failed = append(r.Failed, err)
		case did == "":
			r.Unchanged++
		default:
			r.Changed = append(r.Changed, Change{ID: p.Name(), What: did})
		}
	}
}

// carriedIntent is what this direction would do about a carried ref, for a dry
// run.
func (r *Report) carriedIntent(p issuelib.CarriedPlan) string {
	switch {
	case p.Verdict == issuelib.Diverged:
		if r.Force {
			if r.Pulling {
				return "take the remote side"
			}
			return "take this clone"
		}
		if p.Source {
			return "merge the two sides"
		}
		return "refuse: the mirror is off its source's chain"
	case r.Pulling && p.Verdict == issuelib.RemoteOnly:
		return "create here"
	case r.Pulling && p.Verdict == issuelib.Behind:
		return "bring forward"
	case !r.Pulling && (p.Verdict == issuelib.LocalOnly || p.Verdict == issuelib.Ahead):
		return "send to the remote"
	}
	return ""
}

func shortOrNothing(commit string) string {
	if commit == "" {
		return "nothing"
	}
	return shortSHA(commit)
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

// run carries out one direction over the plans. act writes for one issue and
// says what it did; a plan it would have to force is refused instead, and one
// that fails is recorded and does not stop the rest.
func (r *Report) run(s issuelib.Store, plans []issuelib.IssuePlan, act func(issuelib.IssuePlan) (string, error)) {
	for _, p := range plans {
		if p.OldClient {
			r.OldClient = append(r.OldClient, p.XIDR)
		}
		if r.DryRun {
			if what := r.intent(p); what != "" {
				r.Changed = append(r.Changed, Change{ID: p.XIDR, What: fmt.Sprintf("%s (%s)", what, p.Verdict)})
			} else {
				r.Unchanged++
			}
			continue
		}
		did, err := act(p)
		switch {
		case errors.Is(err, issuelib.ErrDiverged):
			r.Refused = append(r.Refused, r.refusal(s, p, err.Error()))
		case err != nil:
			r.Failed = append(r.Failed, err)
		case did == "":
			r.Unchanged++
		default:
			r.Changed = append(r.Changed, Change{ID: p.XIDR, What: did})
		}
	}
}

// refusal is what a person needs to decide one refused issue.
func (r *Report) refusal(s issuelib.Store, p issuelib.IssuePlan, reason string) Refusal {
	ref := Refusal{ID: p.XIDR, Reason: reason, Here: "nothing", There: "nothing", Split: p.Split}
	if p.Local != nil {
		ref.Here = shortSHA(p.Local.Commit)
		if issue, _, err := s.GetByRef(p.Local.Ref); err == nil {
			ref.Title = issue.Title
		}
	}
	if p.R != nil {
		ref.There = shortSHA(p.R.Commit)
	}
	return ref
}

// intent is what this direction would do about the plan, for a dry run: the
// shape of the answer, without the commits a real run would report.
func (r *Report) intent(p issuelib.IssuePlan) string {
	if p.Verdict == issuelib.Diverged {
		if r.Force {
			if r.Pulling {
				return "take the remote side"
			}
			return "take this clone"
		}
		return "merge the two sides"
	}
	if r.Pulling {
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

// shortSHA abbreviates a commit for a message to a person.
func shortSHA(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
