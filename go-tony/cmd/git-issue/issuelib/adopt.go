package issuelib

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Adoption: taking over what a client of the layout before this generation left
// in this clone.
//
// A client older than the generation (see namespace.go) knows only the gen0
// prefixes, and goes on writing them: it is the binary someone has not upgraded,
// or the one they ran yesterday. Its work is not lost and is not left where this
// generation cannot see it. Adoption is local and one-directional -- refs in this
// clone are renamed, nothing is sent anywhere -- and a remote is migrated by an
// ordinary push instead, under the rule that decides every other push.
//
// No commit is rewritten. A ref is recreated at the commit it was already at, so
// every SHA recorded elsewhere, every "Issue:" trailer and every id keeps
// resolving -- which is what the numeric-id migration could not say for itself.

// refAt is a ref and the commit it points at.
type refAt struct {
	ref    string
	commit string
}

// refsAt lists the refs matching any of the patterns, with their commits, in
// git's order. A pattern matching nothing contributes nothing, and neither does
// a directory that is not a git repository -- which is what makes adoption a
// no-op outside one.
func refsAt(patterns ...string) []refAt {
	var found []refAt
	for _, pattern := range patterns {
		out, err := exec.Command("git", "for-each-ref", "--format=%(refname) %(objectname)", pattern).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			ref, commit, ok := strings.Cut(line, " ")
			if !ok {
				continue
			}
			found = append(found, refAt{ref: ref, commit: commit})
		}
	}
	return found
}

// deleteRef deletes ref, provided it is still at commit: the compare-and-swap
// setRef makes for a write, for a removal.
func (s *GitStore) deleteRef(ref, commit string) error {
	cmd := exec.Command("git", "update-ref", "-d", ref, commit)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete %s: %w: %s", ref, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// AdoptGen0 moves every issue this clone holds under the gen0 prefixes into this
// generation's, and folds a gen0 reverse index into this one's.
//
// Per issue, against what this generation already holds for the same id: nothing
// held, so the gen0 tip becomes it, in the status its namespace said; what is
// held already carries the gen0 tip, so the gen0 ref is dropped; the gen0 tip is
// ahead, so it is taken, status and all; or the two have diverged, which no
// rename can settle -- both are left, the issue is named, and a sync merges them.
//
// It is idempotent: a second run finds nothing to adopt and says nothing.
func (s *GitStore) AdoptGen0() error {
	adopted := 0
	for _, gen0 := range refsAt(Gen0OpenPrefix+"*", Gen0ClosedPrefix+"*") {
		xidr, err := XIDRFromRef(gen0.ref)
		if err != nil {
			continue
		}
		want := RefForXIDR(xidr)
		if IsClosedRef(gen0.ref) {
			want = ClosedRefForXIDR(xidr)
		}

		// What this generation holds for the id, in whichever status.
		var held *refAt
		if his := refsAt(RefForXIDR(xidr), ClosedRefForXIDR(xidr)); len(his) > 0 {
			held = &his[0]
		}

		switch {
		case held == nil:
			if err := s.setRef(want, gen0.commit, zeroSHA); err != nil {
				return fmt.Errorf("adopting %s: %w", FormatID(xidr), err)
			}
		case held.commit == gen0.commit || s.isAncestor(gen0.commit, held.commit):
			// Already carried forward, by an earlier adoption or by a sync.
		case s.isAncestor(held.commit, gen0.commit):
			// The gen0 ref is the later of the two: take its commit and its
			// status, which means moving the ref when the status differs.
			if held.ref != want {
				if err := s.deleteRef(held.ref, held.commit); err != nil {
					return fmt.Errorf("adopting %s: %w", FormatID(xidr), err)
				}
				if err := s.setRef(want, gen0.commit, zeroSHA); err != nil {
					return fmt.Errorf("adopting %s: %w", FormatID(xidr), err)
				}
			} else if err := s.setRef(want, gen0.commit, held.commit); err != nil {
				return fmt.Errorf("adopting %s: %w", FormatID(xidr), err)
			}
		default:
			fmt.Fprintf(s.out, "Warning: %s was edited on both sides of an upgrade; "+
				"%s and %s are both kept, and a sync merges them.\n",
				FormatID(xidr), shortSHA(held.commit), shortSHA(gen0.commit))
			continue
		}

		if err := s.deleteRef(gen0.ref, gen0.commit); err != nil {
			return fmt.Errorf("adopting %s: %w", FormatID(xidr), err)
		}
		adopted++
	}

	notes, err := s.adoptGen0Notes()
	if err != nil {
		return err
	}
	if adopted > 0 || notes {
		fmt.Fprintf(s.out, "Adopted %d issue(s) written by an older git-issue.\n", adopted)
	}
	return nil
}

// adoptGen0Notes folds a gen0 reverse index into this generation's and drops it,
// and says whether there was one. The two are unrelated histories -- each was
// started by a different client -- which the union strategy merges all the same,
// note by note and line by line.
func (s *GitStore) adoptGen0Notes() (bool, error) {
	gen0 := refsAt(Gen0NotesRef)
	if len(gen0) == 0 {
		return false, nil
	}
	if len(refsAt(NotesRef)) == 0 {
		if err := s.setRef(NotesRef, gen0[0].commit, zeroSHA); err != nil {
			return false, fmt.Errorf("adopting the reverse index: %w", err)
		}
	} else {
		cmd := exec.Command("git", "notes", "--ref="+NotesRef, "merge", "-s", "union", Gen0NotesRef)
		if out, err := cmd.CombinedOutput(); err != nil {
			return false, fmt.Errorf("failed to merge the older reverse index: %s",
				strings.TrimSpace(string(out)))
		}
	}
	if err := s.deleteRef(Gen0NotesRef, gen0[0].commit); err != nil {
		return false, fmt.Errorf("adopting the reverse index: %w", err)
	}
	return true, nil
}

// adoptOnce adopts at most once per store, for the reads everything goes
// through. A failure here is a warning and not the caller's error: a listing
// that cannot adopt should still list what it can.
//
// Anything that has just put gen0 refs into the clone, or is about to decide
// from local refs, calls AdoptGen0 directly instead -- this once may be long
// spent by then, and what arrived after it would sit unadopted and unseen.
func (s *GitStore) adoptOnce() {
	s.adopted.Do(func() {
		if err := s.AdoptGen0(); err != nil {
			fmt.Fprintf(s.out, "Warning: failed to adopt refs written by an older git-issue: %v\n", err)
		}
	})
}

// shortSHA abbreviates a commit for a message to a person.
func shortSHA(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
