package issuelib

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Carried refs: the mirrors (ext/) and the sources (sources/) go with a
// repository's issues -- carried by push to this repository's origin, taken by
// pull from it -- because a clone that lacked a mirror could not follow a
// relation to it. They are not issues: a mirror is a point on another
// repository's chain, a source a URL and a time, so what a sync decides about
// them is simpler than an issue's plan and kept beside it rather than in it.
//
// A mirror's two sides are two points on one chain, so one carries the other,
// or the mirror has left the source's chain, which nothing here does and a
// refresh from the source puts right. A source's two sides can both have moved
// (two clones fetched at different times, or one re-recorded the URL) and are
// merged field by field, with both parents kept, so the next sync on either side
// fast-forwards: the URL is the side that changed it, and the fetch time is the
// later one. Nothing is pushed to a source: push names this repository's origin,
// and a source is somewhere else.

// CarriedPlan is what a sync would do about one carried ref.
type CarriedPlan struct {
	Ref     string // the same name here and on the remote
	Local   string // this clone's commit, "" for none
	Remote  string // the remote's, "" for none
	Verdict Verdict
	Source  bool // a sources/ ref, as opposed to a mirror
}

// Name is the ref as a person reads it: "ext b/<xidr>" or "source b".
func (p CarriedPlan) Name() string {
	if p.Source {
		return "source " + strings.TrimPrefix(p.Ref, SourcesPrefix)
	}
	return "ext " + strings.TrimPrefix(p.Ref, ExtPrefix)
}

// carriedSources is how the two carried namespaces are tracked from a remote.
func carriedSources(remote string) []struct{ local, tracking string } {
	return []struct{ local, tracking string }{
		{ExtPrefix, TrackingExtPrefix(remote)},
		{SourcesPrefix, TrackingSourcesPrefix(remote)},
	}
}

// PlanCarried answers what a sync with remote would do about every mirror and
// source either side holds, sorted by ref. It writes nothing.
func (s *GitStore) PlanCarried(remote string) ([]CarriedPlan, error) {
	byRef := map[string]*CarriedPlan{}
	for _, ns := range carriedSources(remote) {
		for _, r := range s.refsAt(ns.local + "*") {
			p := &CarriedPlan{Ref: r.ref, Local: r.commit, Source: ns.local == SourcesPrefix}
			byRef[r.ref] = p
		}
		for _, r := range s.refsAt(ns.tracking + "*") {
			ref := ns.local + strings.TrimPrefix(r.ref, ns.tracking)
			p, ok := byRef[ref]
			if !ok {
				p = &CarriedPlan{Ref: ref, Source: ns.local == SourcesPrefix}
				byRef[ref] = p
			}
			p.Remote = r.commit
		}
	}
	// refsAt with "*" does not cross a slash, and a mirror is ext/<source>/<xidr>.
	for _, r := range s.refsAt(ExtPrefix + "*/*") {
		if _, ok := byRef[r.ref]; !ok {
			byRef[r.ref] = &CarriedPlan{Ref: r.ref, Local: r.commit}
		} else {
			byRef[r.ref].Local = r.commit
		}
	}
	for _, r := range s.refsAt(TrackingExtPrefix(remote) + "*/*") {
		ref := ExtPrefix + strings.TrimPrefix(r.ref, TrackingExtPrefix(remote))
		p, ok := byRef[ref]
		if !ok {
			p = &CarriedPlan{Ref: ref}
			byRef[ref] = p
		}
		p.Remote = r.commit
	}

	refs := make([]string, 0, len(byRef))
	for ref := range byRef {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	plans := make([]CarriedPlan, 0, len(refs))
	for _, ref := range refs {
		p := byRef[ref]
		var local, remote *Tip
		if p.Local != "" {
			local = &Tip{Ref: ref, Commit: p.Local}
		}
		if p.Remote != "" {
			remote = &Tip{Ref: ref, Commit: p.Remote}
		}
		p.Verdict = s.verdict(local, remote, false)
		plans = append(plans, *p)
	}
	return plans, nil
}

// ApplyCarriedPull brings this clone's carried ref to what the remote holds, and
// says what it did. A diverged source is merged; a diverged mirror is refused,
// since the two sides should be points on one chain and are not -- a refresh
// from the source settles it -- unless forced, which takes the remote's.
func (s *GitStore) ApplyCarriedPull(p CarriedPlan, force bool) (string, error) {
	switch p.Verdict {
	case LocalOnly, Ahead, Equal:
		return "", nil
	case RemoteOnly:
		if err := s.setRef(p.Ref, p.Remote, zeroSHA); err != nil {
			return "", err
		}
		return "created at " + shortSHA(p.Remote), nil
	case Behind:
		if err := s.setRef(p.Ref, p.Remote, p.Local); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s..%s", shortSHA(p.Local), shortSHA(p.Remote)), nil
	}
	// Diverged.
	if force {
		if err := s.setRef(p.Ref, p.Remote, p.Local); err != nil {
			return "", err
		}
		return fmt.Sprintf("forced to %s (%s is in the reflog)", shortSHA(p.Remote), shortSHA(p.Local)), nil
	}
	if !p.Source {
		return "", fmt.Errorf("%w: the mirror is not on the source's chain here and there both; `git issue ext refresh` from the source settles it", ErrDiverged)
	}
	merged, err := s.mergeSources(p.Ref, p.Local, p.Remote)
	if err != nil {
		return "", err
	}
	if err := s.setRef(p.Ref, merged, p.Local); err != nil {
		return "", err
	}
	return fmt.Sprintf("merged %s and %s", shortSHA(p.Local), shortSHA(p.Remote)), nil
}

// mergeSources merges two versions of a source, field by field, and the commit
// has both as parents. The URL is merged three-way: it is the side that changed
// it since the two last agreed, and kept when neither did. Two sides that both
// changed it, to different URLs, are refused and named, since which repository
// a source is, is for a person to say; so are sides with no history in common
// that disagree, and histories whose bases do not agree on which side changed
// it. The fetch time is the later of the two, since each says when that side
// last fetched its mirrors and the later one is when they were last fetched.
func (s *GitStore) mergeSources(ref, ours, theirs string) (string, error) {
	ourRec, ourAt, err := s.sourceAt(ours)
	if err != nil {
		return "", fmt.Errorf("%s at %s: %w", ref, shortSHA(ours), err)
	}
	theirRec, theirAt, err := s.sourceAt(theirs)
	if err != nil {
		return "", fmt.Errorf("%s at %s: %w", ref, shortSHA(theirs), err)
	}
	rec := ourRec
	if theirAt.After(ourAt) {
		rec.Fetched = theirRec.Fetched
	}
	if ourRec.URL != theirRec.URL {
		bases, err := s.mergeBases(ours, theirs)
		if err != nil {
			return "", err
		}
		weKept, theyKept := false, false
		var was []string
		for _, base := range bases {
			baseRec, _, err := s.sourceAt(base)
			if err != nil {
				return "", fmt.Errorf("%s at %s: %w", ref, shortSHA(base), err)
			}
			weKept = weKept || baseRec.URL == ourRec.URL
			theyKept = theyKept || baseRec.URL == theirRec.URL
			was = append(was, shortSHA(base)+", where it was "+baseRec.URL)
		}
		switch {
		case weKept && !theyKept:
			rec.URL = theirRec.URL
		case theyKept && !weKept:
		default:
			why := "the two were recorded with no history in common"
			switch {
			case len(bases) > 0 && !weKept:
				why = "both changed since " + strings.Join(was, " and ")
			case len(bases) > 0:
				why = "and the points where they last agreed do not say which changed it: " + strings.Join(was, " and ")
			}
			name := strings.TrimPrefix(ref, SourcesPrefix)
			return "", fmt.Errorf("%w: source %s is %s here and %s there, %s; `git issue ext add %s` with the one to keep, then the sync again, settles it",
				ErrDiverged, name, ourRec.URL, theirRec.URL, why, name)
		}
	}
	return s.commitSource(rec, fmt.Sprintf("merge: %s %s", shortSHA(ours), shortSHA(theirs)), ours, theirs)
}

// mergeBases answers the best common ancestors of two commits, none when they
// share no history. A git that fails for another reason is that error, not an
// answer of none.
func (s *GitStore) mergeBases(ours, theirs string) ([]string, error) {
	cmd := s.git("merge-base", "--all", ours, theirs)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("git merge-base %s %s: %w: %s", shortSHA(ours), shortSHA(theirs), err, strings.TrimSpace(stderr.String()))
	}
	return strings.Fields(string(out)), nil
}

// ApplyCarriedPushes sends every carried ref the remote lacks or is behind on,
// in one atomic push with a lease on what the last fetch saw, and answers what
// it did about each, in the order of plans. A diverged source is merged first
// and the merge sent; a diverged mirror is refused unless forced.
func (s *GitStore) ApplyCarriedPushes(remote string, plans []CarriedPlan, force bool) []PushResult {
	results := make([]PushResult, len(plans))
	var ops []*pushOp
	for i, p := range plans {
		switch p.Verdict {
		case RemoteOnly, Behind, Equal:
			continue
		case Diverged:
			switch {
			case force:
			case p.Source:
				merged, err := s.mergeSources(p.Ref, p.Local, p.Remote)
				if err != nil {
					results[i].Err = err
					continue
				}
				if err := s.setRef(p.Ref, merged, p.Local); err != nil {
					results[i].Err = err
					continue
				}
				p.Local = merged
			default:
				results[i].Err = fmt.Errorf("%w: the mirror is not on the source's chain here and there both; `git issue ext refresh` from the source settles it", ErrDiverged)
				continue
			}
		}
		if p.Local == "" {
			continue
		}
		op := &pushOp{
			xidr:     p.Name(),
			leases:   []string{"--force-with-lease=" + p.Ref + ":" + p.Remote},
			refspecs: []string{p.Ref + ":" + p.Ref},
			targets:  []string{p.Ref},
			commits:  []string{p.Local},
			result:   &results[i],
		}
		if p.Remote == "" {
			op.did = "created at " + shortSHA(p.Local)
		} else {
			op.did = fmt.Sprintf("%s..%s", shortSHA(p.Remote), shortSHA(p.Local))
		}
		ops = append(ops, op)
	}
	for start := 0; start < len(ops); start += pushBatch {
		s.sendPushes(remote, ops[start:min(start+pushBatch, len(ops))])
	}
	return results
}
