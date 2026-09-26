package issuelib

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/parse"
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
// (two clones fetched at different times) and are merged: the later fetch is
// what source.tony says, with both parents kept, so the next sync on either side
// fast-forwards. Nothing is pushed to a source: push names this repository's
// origin, and a source is somewhere else.

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

// mergeSources merges two versions of a source: the later fetch is what
// source.tony says, and the commit has both as parents.
func (s *GitStore) mergeSources(ref, ours, theirs string) (string, error) {
	read := func(commit string) (sourceRecord, time.Time, error) {
		raw, err := s.git("show", commit+":source.tony").Output()
		if err != nil {
			return sourceRecord{}, time.Time{}, fmt.Errorf("failed to read %s at %s: %w", ref, shortSHA(commit), err)
		}
		node, err := parse.Parse(raw)
		if err != nil {
			return sourceRecord{}, time.Time{}, err
		}
		var rec sourceRecord
		if err := gomap.FromTonyIR(node, &rec); err != nil {
			return sourceRecord{}, time.Time{}, err
		}
		fetched, _ := time.Parse(time.RFC3339, rec.Fetched)
		return rec, fetched, nil
	}
	ourRec, ourAt, err := read(ours)
	if err != nil {
		return "", err
	}
	theirRec, theirAt, err := read(theirs)
	if err != nil {
		return "", err
	}
	rec := ourRec
	if theirAt.After(ourAt) {
		rec = theirRec
	}
	node, err := gomap.ToTonyIR(rec)
	if err != nil {
		return "", err
	}
	hash := s.git("hash-object", "-w", "--stdin")
	hash.Stdin = strings.NewReader(encode.MustString(node))
	hashOut, err := hash.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write source.tony: %w", err)
	}
	mktree := s.git("mktree")
	mktree.Stdin = strings.NewReader(fmt.Sprintf("100644 blob %s\tsource.tony\n", strings.TrimSpace(string(hashOut))))
	treeOut, err := mktree.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write the source's tree: %w", err)
	}
	commit := s.git("commit-tree", strings.TrimSpace(string(treeOut)), "-p", ours, "-p", theirs,
		"-m", fmt.Sprintf("merge: %s %s", shortSHA(ours), shortSHA(theirs)))
	var stderr bytes.Buffer
	commit.Stderr = &stderr
	commitOut, err := commit.Output()
	if err != nil {
		return "", fmt.Errorf("failed to merge %s: %w: %s", ref, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(commitOut)), nil
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
