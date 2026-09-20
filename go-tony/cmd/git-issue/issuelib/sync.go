package issuelib

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Sync: what a push or a pull should do to each issue, and doing it without
// overwriting anyone.
//
// A remote's refs are fetched into this clone first, under
// refs/git-issues/<gen>/remotes/<remote>/, as refs/remotes/ is for branches.
// That is what makes ahead, behind and diverged local questions -- git answers
// them with merge-base, and nothing has to guess from the network. Planning
// reads those refs and the local ones and writes nothing, so it is also what
// --dry-run prints and what the tests drive.
//
// Every write to a remote then carries a lease on what the tracking ref said:
// --force-with-lease refuses the write if the remote has moved since the fetch.
// That is the compare-and-swap the store has always made locally (setRef) and
// the transport did not have, which is why the last writer of an issue used to
// win.

// Verdict is what an issue's two sides amount to, and it decides what each
// direction does about it. Status is not part of it: an issue is one chain
// whichever namespace each side keeps it in, so these compare commits, and the
// namespace follows the tip that wins.
type Verdict int

const (
	// RemoteOnly: this clone has never held the issue.
	RemoteOnly Verdict = iota
	// LocalOnly: the remote has never held it.
	LocalOnly
	// Equal: the same commit on both sides.
	Equal
	// Behind: the remote's tip carries this clone's.
	Behind
	// Ahead: this clone's tip carries the remote's.
	Ahead
	// Diverged: neither carries the other, or the remote's own tips disagree.
	// The only verdict that can lose work, and so the only one refused.
	Diverged
)

func (v Verdict) String() string {
	switch v {
	case RemoteOnly:
		return "remote only"
	case LocalOnly:
		return "local only"
	case Equal:
		return "equal"
	case Behind:
		return "behind"
	case Ahead:
		return "ahead"
	}
	return "diverged"
}

// Tip is one end of an issue's chain: the ref, the commit, and what the ref's
// namespace says about it.
//
// For a remote tip, Ref is the name the ref has ON THE REMOTE -- what a push
// writes and what a lease is taken against -- rather than the tracking ref it
// was read from. The two names are the same for an issue of this generation,
// and differ for a gen0 one.
type Tip struct {
	Ref    string
	Commit string
	Closed bool
	Gen0   bool
}

// IssuePlan is one issue's two sides and what follows from them. It is the whole
// of what a sync decides, and it is decided before anything is written.
type IssuePlan struct {
	XIDR  string
	Local *Tip // nil when this clone does not hold the issue
	// Remote is every tip the remote has for the issue: up to four, since it
	// may hold the issue open or closed in either generation.
	Remote []Tip
	// R is the remote's tip: the one every other remote tip is carried by. Nil
	// when the remote does not hold the issue, or when Split.
	R       *Tip
	Verdict Verdict
	// Split says the remote's own tips disagree, which nothing but a client of
	// another generation pushing after this one can cause.
	Split bool
	// OldClient says a gen0 ref of this issue is on a remote that has otherwise
	// moved to this generation: someone is still running a git-issue too old to
	// see the current refs.
	OldClient bool
}

// issueRef is where an issue of the given status lives, in the given
// generation. It is the same name in a clone and on a remote, which is why
// pushing one is a refspec from a name to itself.
func issueRef(xidr string, closed, gen0 bool) string {
	switch {
	case gen0 && closed:
		return Gen0ClosedRefForXIDR(xidr)
	case gen0:
		return Gen0RefForXIDR(xidr)
	case closed:
		return ClosedRefForXIDR(xidr)
	}
	return RefForXIDR(xidr)
}

// trackingSources is where a remote's four kinds of issue ref are kept in this
// clone, and what each says about the issues in it.
func trackingSources(remote string) []struct {
	prefix       string
	closed, gen0 bool
} {
	return []struct {
		prefix       string
		closed, gen0 bool
	}{
		{TrackingOpenPrefix(remote), false, false},
		{TrackingClosedPrefix(remote), true, false},
		{TrackingGen0OpenPrefix(remote), false, true},
		{TrackingGen0ClosedPrefix(remote), true, true},
	}
}

// FetchTracking brings this clone's copy of what remote holds up to date, and
// says whether the remote has moved to this generation.
//
// The issue refs go in one fetch so that one round trip answers for all of
// them, and forced, because a tracking ref is a copy of what the remote has and
// not a history of its own. --prune is what makes the copy an answer rather
// than an accumulation: a ref the remote no longer has is one this clone must
// stop believing in, which is how a remote that has been migrated stops looking
// like one holding gen0 issues.
//
// The two notes refs are fetched one at a time because a refspec naming a single
// ref the remote lacks fails the whole fetch it is in, and a remote with no
// reverse index is an ordinary thing.
func (s *GitStore) FetchTracking(remote string) (bool, error) {
	args := []string{"fetch", "--prune", remote}
	for _, src := range trackingSources(remote) {
		from := issueRef("*", src.closed, src.gen0)
		args = append(args, "+"+from+":"+src.prefix+"*")
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil && !quietSyncFailure(string(out)) {
		return false, fmt.Errorf("failed to fetch issues from %s: %s", remote, strings.TrimSpace(string(out)))
	}

	for _, notes := range []struct{ from, to string }{
		{NotesRef, TrackingNotesRef(remote)},
		{Gen0NotesRef, TrackingGen0NotesRef(remote)},
	} {
		out, err := exec.Command("git", "fetch", remote, "+"+notes.from+":"+notes.to).CombinedOutput()
		if err == nil {
			continue
		}
		if !quietSyncFailure(string(out)) {
			return false, fmt.Errorf("failed to fetch %s from %s: %s", notes.from, remote, strings.TrimSpace(string(out)))
		}
		// The remote does not have it, so neither should this clone's copy.
		if held := refsAt(notes.to); len(held) == 1 {
			if err := s.deleteRef(notes.to, held[0].commit); err != nil {
				return false, err
			}
		}
	}
	return s.remoteIsMigrated(remote), nil
}

// remoteIsMigrated says the last fetch found this generation on the remote: an
// issue ref of it, or its reverse index. A gen0 ref on such a remote was pushed
// by a client too old to see either.
func (s *GitStore) remoteIsMigrated(remote string) bool {
	return len(refsAt(
		TrackingOpenPrefix(remote)+"*",
		TrackingClosedPrefix(remote)+"*",
		TrackingNotesRef(remote),
	)) > 0
}

// PlanSync answers what a sync with remote would do to each issue either side
// holds, sorted by id. It reads refs and asks git about ancestry, and writes
// nothing -- so it is what --dry-run prints, and a plan can be read twice.
//
// What an older git-issue left in this clone is adopted first, directly rather
// than through the once a read takes, so no plan is made about a ref this
// generation is about to rename. A pair adoption could not settle -- the two
// diverged -- is left in gen0 and is not planned for; the issue's own ref is.
func (s *GitStore) PlanSync(remote string) ([]IssuePlan, error) {
	if err := s.AdoptGen0(); err != nil {
		return nil, err
	}

	local := map[string]Tip{}
	for _, r := range refsAt(OpenPrefix+"*", ClosedPrefix+"*") {
		xidr, err := XIDRFromRef(r.ref)
		if err != nil {
			continue
		}
		local[xidr] = Tip{Ref: r.ref, Commit: r.commit, Closed: IsClosedRef(r.ref)}
	}

	remoteTips := map[string][]Tip{}
	for _, src := range trackingSources(remote) {
		for _, r := range refsAt(src.prefix + "*") {
			xidr := strings.TrimPrefix(r.ref, src.prefix)
			remoteTips[xidr] = append(remoteTips[xidr], Tip{
				Ref:    issueRef(xidr, src.closed, src.gen0),
				Commit: r.commit,
				Closed: src.closed,
				Gen0:   src.gen0,
			})
		}
	}

	seen := map[string]bool{}
	var ids []string
	for xidr := range local {
		seen[xidr] = true
		ids = append(ids, xidr)
	}
	for xidr := range remoteTips {
		if !seen[xidr] {
			ids = append(ids, xidr)
		}
	}
	sort.Strings(ids)

	migrated := s.remoteIsMigrated(remote)
	plans := make([]IssuePlan, 0, len(ids))
	for _, xidr := range ids {
		p := IssuePlan{XIDR: xidr, Remote: remoteTips[xidr]}
		if tip, ok := local[xidr]; ok {
			p.Local = &tip
		}
		p.R, p.Split = s.remoteTip(p.Remote)
		p.Verdict = s.verdict(p.Local, p.R, p.Split)
		for _, tip := range p.Remote {
			if tip.Gen0 && migrated {
				p.OldClient = true
			}
		}
		plans = append(plans, p)
	}
	return plans, nil
}

// remoteTip is the remote's tip for an issue: the one every other tip it holds
// is carried by. Where two of them are the same commit in an open and a closed
// namespace the closed one is it, which is the rule a repository holding both
// has always been read by. Where no tip carries all the others the remote is
// split, and there is no answer to give.
func (s *GitStore) remoteTip(tips []Tip) (*Tip, bool) {
	if len(tips) == 0 {
		return nil, false
	}
	var best *Tip
	for i := range tips {
		carriesAll := true
		for j := range tips {
			if i != j && !s.isAncestor(tips[j].Commit, tips[i].Commit) {
				carriesAll = false
				break
			}
		}
		if !carriesAll {
			continue
		}
		if best == nil || (tips[i].Commit == best.Commit && tips[i].Closed && !best.Closed) {
			best = &tips[i]
		}
	}
	if best == nil {
		return nil, true
	}
	return best, false
}

func (s *GitStore) verdict(local, r *Tip, split bool) Verdict {
	switch {
	case split:
		return Diverged
	case local == nil:
		return RemoteOnly
	case r == nil:
		return LocalOnly
	case local.Commit == r.Commit:
		return Equal
	case s.isAncestor(local.Commit, r.Commit):
		return Behind
	case s.isAncestor(r.Commit, local.Commit):
		return Ahead
	}
	return Diverged
}

// ErrDiverged is what applying a diverged plan answers when nothing has said
// which side wins. The two chains both hold work, and picking one silently is
// the defect this whole file is about.
var ErrDiverged = fmt.Errorf("the issue was edited on both sides")

// ApplyPull brings this clone's ref for one issue to what the remote holds, and
// says what it did. force decides a divergence, and decides nothing else: it
// takes the remote's tip and leaves this clone's in the ref's reflog.
func (s *GitStore) ApplyPull(p IssuePlan, force bool) (string, error) {
	switch p.Verdict {
	case LocalOnly, Ahead:
		return "", nil
	case Diverged:
		if !force {
			return "", ErrDiverged
		}
	}
	if p.R == nil {
		return "", nil
	}

	switch p.Verdict {
	case Equal:
		// The same commit, in different namespaces: the status is news even
		// when the chain is not.
		if p.Local != nil && p.Local.Closed == p.R.Closed {
			return "", nil
		}
		if err := s.putLocal(p, p.R.Commit, p.R.Closed); err != nil {
			return "", err
		}
		return "reopened here", nil
	case RemoteOnly:
		if err := s.putLocal(p, p.R.Commit, p.R.Closed); err != nil {
			return "", err
		}
		return "created at " + shortSHA(p.R.Commit), nil
	case Behind:
		was := p.Local.Commit
		if err := s.putLocal(p, p.R.Commit, p.R.Closed); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s..%s", shortSHA(was), shortSHA(p.R.Commit)), nil
	default: // Diverged, with force
		was := ""
		if p.Local != nil {
			was = shortSHA(p.Local.Commit) + " "
		}
		if err := s.putLocal(p, p.R.Commit, p.R.Closed); err != nil {
			return "", err
		}
		return fmt.Sprintf("forced to %s (%sis in the reflog)", shortSHA(p.R.Commit), was), nil
	}
}

// putLocal points this clone's ref for the issue at commit, in the status closed
// names, moving it out of the other namespace when that is where it was. An
// issue is in one namespace or the other and never both, which is what a listing
// reads a status from.
func (s *GitStore) putLocal(p IssuePlan, commit string, closed bool) error {
	want := issueRef(p.XIDR, closed, false)
	if p.Local == nil {
		return s.setRef(want, commit, zeroSHA)
	}
	if p.Local.Ref == want {
		if p.Local.Commit == commit {
			return nil
		}
		return s.setRef(want, commit, p.Local.Commit)
	}
	if err := s.deleteRef(p.Local.Ref, p.Local.Commit); err != nil {
		return err
	}
	return s.setRef(want, commit, zeroSHA)
}

// ApplyPush makes the remote right about one issue, and says what it did.
//
// Right means: the remote ends holding exactly one ref for the issue, this
// generation's, in the status this clone has it in, at this clone's tip. So the
// push both sends the issue and removes whatever else the remote had for it --
// the other status, and either gen0 ref -- which is what mirrors a close, and
// what migrates an issue that was only ever on the remote in gen0.
//
// It is safe precisely when this clone's tip carries every tip the remote has,
// which is what the verdict says. The whole issue goes in one atomic push, so
// its refs change together or not at all, and every one of them carries a lease
// on what the tracking ref said: if the remote moved since the fetch, nothing
// is written and the caller is told.
func (s *GitStore) ApplyPush(remote string, p IssuePlan, force bool) (string, error) {
	switch p.Verdict {
	case RemoteOnly, Behind:
		return "", nil
	case Diverged:
		if !force {
			return "", ErrDiverged
		}
	}
	if p.Local == nil {
		return "", nil
	}

	want := issueRef(p.XIDR, p.Local.Closed, false)
	held := ""
	for _, tip := range p.Remote {
		if tip.Ref == want {
			held = tip.Commit
		}
	}

	var leases, refspecs []string
	sent := held != p.Local.Commit
	if sent {
		refspecs = append(refspecs, p.Local.Ref+":"+want)
		leases = append(leases, "--force-with-lease="+want+":"+held)
	}
	dropped := 0
	for _, tip := range p.Remote {
		if tip.Ref == want {
			continue
		}
		refspecs = append(refspecs, ":"+tip.Ref)
		leases = append(leases, "--force-with-lease="+tip.Ref+":"+tip.Commit)
		dropped++
	}
	if len(refspecs) == 0 {
		return "", nil
	}

	args := append([]string{"push", "--atomic"}, leases...)
	args = append(args, remote)
	args = append(args, refspecs...)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to push %s to %s: %s",
			FormatID(p.XIDR), remote, strings.TrimSpace(string(out)))
	}

	var did []string
	if sent {
		if held == "" {
			did = append(did, "created at "+shortSHA(p.Local.Commit))
		} else {
			did = append(did, fmt.Sprintf("%s..%s", shortSHA(held), shortSHA(p.Local.Commit)))
		}
	}
	if dropped > 0 {
		did = append(did, fmt.Sprintf("dropped %d other ref(s)", dropped))
	}
	return strings.Join(did, ", "), nil
}

// SyncNotes folds what the remote's reverse indexes hold into this clone's, and
// with push set sends the result back and clears the remote's gen0 one.
//
// The union strategy is what a reverse index wants and what git has always had:
// two clones that linked different commits each hold notes the other does not,
// and neither is a conflict. The merged ref carries both sides, so the push that
// follows is a fast-forward the remote can take.
func (s *GitStore) SyncNotes(remote string, push bool) error {
	for _, tracking := range []string{TrackingNotesRef(remote), TrackingGen0NotesRef(remote)} {
		held := refsAt(tracking)
		if len(held) == 0 {
			continue
		}
		if len(refsAt(NotesRef)) == 0 {
			if err := s.setRef(NotesRef, held[0].commit, zeroSHA); err != nil {
				return fmt.Errorf("failed to take the reverse index of %s: %w", remote, err)
			}
			continue
		}
		cmd := exec.Command("git", "notes", "--ref="+NotesRef, "merge", "-s", "union", tracking)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to merge the reverse index of %s: %s",
				remote, strings.TrimSpace(string(out)))
		}
	}
	if !push {
		return nil
	}

	var leases, refspecs []string
	if local := refsAt(NotesRef); len(local) == 1 {
		held := ""
		if tracking := refsAt(TrackingNotesRef(remote)); len(tracking) == 1 {
			held = tracking[0].commit
		}
		if held != local[0].commit {
			refspecs = append(refspecs, NotesRef+":"+NotesRef)
			leases = append(leases, "--force-with-lease="+NotesRef+":"+held)
		}
	}
	if tracking := refsAt(TrackingGen0NotesRef(remote)); len(tracking) == 1 {
		refspecs = append(refspecs, ":"+Gen0NotesRef)
		leases = append(leases, "--force-with-lease="+Gen0NotesRef+":"+tracking[0].commit)
	}
	if len(refspecs) == 0 {
		return nil
	}

	args := append([]string{"push", "--atomic"}, leases...)
	args = append(args, remote)
	args = append(args, refspecs...)
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push the reverse index to %s: %s",
			remote, strings.TrimSpace(string(out)))
	}
	return nil
}
