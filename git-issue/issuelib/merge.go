package issuelib

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// Merging two chains of one issue.
//
// Two clones that both edited an issue hold chains from a common root, each
// with commits the other does not. Nothing about that is a conflict in the
// ordinary case: discussion/ unions by construction, since its names are
// <timestamp>-<hash> and cannot collide, and meta.tony's lists are sets, each
// side's additions and removals kept. What needs deciding is the status, and
// the rule for it is below; the one conflict in meta.tony is a label key the two
// sides set to different values.
//
// The result is a commit with both tips as parents, so the next sync sees a
// fast-forward from either side and the two clones converge without anyone
// choosing a loser.

// MergeIssue three-way merges the chains at ours and theirs, which share base,
// and answers the merge commit.
//
// The trees are merged by git, and meta.tony is then replaced by one merged by
// value -- a text merge of it would be a merge of a generated file, which is
// how two orderings of one list become a conflict about nothing. A conflict in
// any other path is answered as one: two people rewrote the same description,
// and no rule here is better than asking them.
func (s *GitStore) MergeIssue(base, ours, theirs string) (string, error) {
	tree, err := s.mergeTrees(base, ours, theirs)
	if err != nil {
		return "", err
	}
	meta, err := s.mergeMeta(base, ours, theirs)
	if err != nil {
		return "", err
	}
	return s.commitMerge(tree, meta, ours, theirs)
}

// MergeBase is the commit two chains of an issue last had in common.
func (s *GitStore) MergeBase(ours, theirs string) (string, error) {
	out, err := s.git("merge-base", ours, theirs).Output()
	if err != nil {
		return "", fmt.Errorf("%s and %s share no history", shortSHA(ours), shortSHA(theirs))
	}
	return strings.TrimSpace(string(out)), nil
}

// mergeTrees merges the two trees and answers the merged one. A conflict in
// meta.tony is not one: it is replaced wholesale by mergeMeta, and git reaching
// for text markers in a generated file says nothing about the issue.
func (s *GitStore) mergeTrees(base, ours, theirs string) (string, error) {
	cmd := s.git("merge-tree", "--write-tree", "--merge-base="+base, ours, theirs)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
		return "", fmt.Errorf("failed to merge %s and %s: %w: %s",
			shortSHA(ours), shortSHA(theirs), err, strings.TrimSpace(string(exit.Stderr)))
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", fmt.Errorf("git merge-tree said nothing about %s and %s",
			shortSHA(ours), shortSHA(theirs))
	}
	tree := lines[0]

	var conflicted []string
	for _, line := range lines[1:] {
		if line == "" {
			break // the conflicted entries end, and git's own report begins
		}
		_, path, ok := strings.Cut(line, "\t")
		if !ok || path == "meta.tony" || Contains(conflicted, path) {
			continue
		}
		conflicted = append(conflicted, path)
	}
	if len(conflicted) > 0 {
		return "", fmt.Errorf("%w: %s cannot be merged",
			ErrDiverged, strings.Join(conflicted, ", "))
	}
	return tree, nil
}

// mergeMeta merges the three metadata documents by value.
//
// The lists are sets, merged against the base: what either clone added is
// kept, and what either removed stays removed, so an unlabel is not undone by
// an edit made elsewhere. Labels are merged the same way, with a key=value key
// decided by mergeLabels. id and created are the base's, since neither side may
// change them; updated is the later of the two, which is when the issue last
// moved. status is the one question with two defensible answers, and
// decideStatus settles it.
func (s *GitStore) mergeMeta(base, ours, theirs string) (string, error) {
	baseIssue, err := s.metaAt(base)
	if err != nil {
		return "", err
	}
	ourIssue, err := s.metaAt(ours)
	if err != nil {
		return "", err
	}
	theirIssue, err := s.metaAt(theirs)
	if err != nil {
		return "", err
	}

	merged := *ourIssue
	merged.ID = baseIssue.ID
	merged.Created = baseIssue.Created
	merged.Updated = ourIssue.Updated
	if theirIssue.Updated.After(merged.Updated) {
		merged.Updated = theirIssue.Updated
	}
	merged.Commits = mergeSet(baseIssue.Commits, ourIssue.Commits, theirIssue.Commits)
	merged.Branches = mergeSet(baseIssue.Branches, ourIssue.Branches, theirIssue.Branches)
	merged.RelatedIssues = mergeSet(baseIssue.RelatedIssues, ourIssue.RelatedIssues, theirIssue.RelatedIssues)
	merged.Blocks = mergeSet(baseIssue.Blocks, ourIssue.Blocks, theirIssue.Blocks)
	merged.BlockedBy = mergeSet(baseIssue.BlockedBy, ourIssue.BlockedBy, theirIssue.BlockedBy)
	merged.Duplicates = mergeSet(baseIssue.Duplicates, ourIssue.Duplicates, theirIssue.Duplicates)
	merged.Labels, err = mergeLabels(baseIssue.Labels, ourIssue.Labels, theirIssue.Labels)
	if err != nil {
		return "", err
	}

	status, closedBy, err := s.decideStatus(base, ours, theirs, baseIssue)
	if err != nil {
		return "", err
	}
	merged.Status = status
	merged.ClosedBy = closedBy

	node, err := merged.ToTonyIR()
	if err != nil {
		return "", fmt.Errorf("failed to serialize the merged issue: %w", err)
	}
	return encode.MustString(node), nil
}

// mergeSet merges two of an issue's lists against their base, as sets: ours in
// order, less what theirs removed, then what only theirs added. What ours
// removed is already absent from ours, and not taken back from theirs.
func mergeSet(base, ours, theirs []string) []string {
	out := []string{}
	for _, item := range ours {
		if Contains(base, item) && !Contains(theirs, item) {
			continue
		}
		out = append(out, item)
	}
	for _, item := range theirs {
		if !Contains(base, item) && !Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

// mergeLabels merges labels as mergeSet does, and each key=value key by what
// each side did to it: where one side changed the key -- to another value, or
// by removing it -- that side's answer is taken, and where both changed it to
// the same answer that is taken. Where the sides changed it to different
// answers, which is two people moving one key at once, no rule here is better
// than asking them, and the merge is refused as a conflict in a file is.
//
// Each list is taken to hold one value per key, which singleValued makes true
// of anything metaAt reads.
func mergeLabels(base, ours, theirs []string) ([]string, error) {
	baseVals, ourVals, theirVals := keyValues(base), keyValues(ours), keyValues(theirs)
	won := map[string]string{} // key -> label, absent where the key is removed
	var keys []string
	for _, vals := range []map[string]string{ourVals, theirVals, baseVals} {
		for k := range vals {
			if !Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		b, o, t := baseVals[k], ourVals[k], theirVals[k]
		var l string
		switch {
		case o == t, t == b:
			l = o
		case o == b:
			l = t
		default:
			return nil, fmt.Errorf("%w: label key %s was %s on one side and %s on the other",
				ErrDiverged, k, labelAnswer(o), labelAnswer(t))
		}
		if l != "" {
			won[k] = l
		}
	}

	var plainBase, plainOurs, plainTheirs []string
	for _, pair := range []struct {
		in  []string
		out *[]string
	}{{base, &plainBase}, {ours, &plainOurs}, {theirs, &plainTheirs}} {
		for _, l := range pair.in {
			if _, _, keyed := SplitLabel(l); !keyed {
				*pair.out = append(*pair.out, l)
			}
		}
	}
	plain := mergeSet(plainBase, plainOurs, plainTheirs)

	// Ours in order, then theirs, each keyed label standing where its key first
	// appears, so a merge does not reshuffle what someone is reading.
	out := []string{}
	for _, l := range append(append([]string{}, ours...), theirs...) {
		k, _, keyed := SplitLabel(l)
		switch {
		case keyed:
			if w, ok := won[k]; ok && !Contains(out, w) {
				out = append(out, w)
			}
		case Contains(plain, l) && !Contains(out, l):
			out = append(out, l)
		}
	}
	return out, nil
}

// keyValues answers a label list's key=value labels, by key.
func keyValues(labels []string) map[string]string {
	vals := map[string]string{}
	for _, l := range labels {
		if k, _, keyed := SplitLabel(l); keyed {
			vals[k] = l
		}
	}
	return vals
}

// labelAnswer says what one side did with a key, for a conflict's message.
func labelAnswer(label string) string {
	if label == "" {
		return "removed"
	}
	_, v, _ := SplitLabel(label)
	return "set to " + v
}

// statusChange is a commit that opened or closed an issue.
type statusChange struct {
	status   string
	closedBy *string
	when     time.Time
}

// decideStatus answers the status the merge takes, and the closing commit that
// goes with it.
//
// The later of the two sides' status changes wins, by the committer date of the
// commit that last made one. Whoever acted second acted knowing more, which is
// the whole of why: a reopen after a close means someone looked again, and
// closing it back would be this tool overruling them. A side that changed
// nothing does not compete, neither side changing anything leaves the base's,
// and a tie closes.
//
// Which client wrote a change is not the rule. A client's version says whether
// its write can be trusted, and nothing about what its author meant -- and two
// clients of the same version disagreeing is the ordinary case, where version
// says nothing at all.
func (s *GitStore) decideStatus(base, ours, theirs string, baseIssue *Issue) (string, *string, error) {
	ourChange, err := s.lastStatusChange(base, ours)
	if err != nil {
		return "", nil, err
	}
	theirChange, err := s.lastStatusChange(base, theirs)
	if err != nil {
		return "", nil, err
	}

	var won *statusChange
	switch {
	case ourChange == nil && theirChange == nil:
		return baseIssue.Status, baseIssue.ClosedBy, nil
	case theirChange == nil:
		won = ourChange
	case ourChange == nil:
		won = theirChange
	case theirChange.when.After(ourChange.when):
		won = theirChange
	case ourChange.when.After(theirChange.when):
		won = ourChange
	case theirChange.status == "closed":
		won = theirChange // a tie closes
	default:
		won = ourChange
	}
	if won.status != "closed" {
		return won.status, nil, nil
	}
	return won.status, won.closedBy, nil
}

// lastStatusChange is the newest commit in base..tip whose status differs from
// its first parent's, or nil where the side never changed it.
func (s *GitStore) lastStatusChange(base, tip string) (*statusChange, error) {
	out, err := s.git("rev-list", base+".."+tip).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read the history of %s: %w", shortSHA(tip), err)
	}
	for _, commit := range strings.Fields(strings.TrimSpace(string(out))) {
		issue, err := s.metaAt(commit)
		if err != nil {
			continue // a commit whose metadata cannot be read says nothing
		}
		parents, err := s.git("rev-parse", commit+"^1").Output()
		if err != nil {
			continue // the root of the chain changed nothing; it began
		}
		before, err := s.metaAt(strings.TrimSpace(string(parents)))
		if err != nil || before.Status == issue.Status {
			continue
		}
		when, err := s.committerDate(commit)
		if err != nil {
			return nil, err
		}
		return &statusChange{status: issue.Status, closedBy: issue.ClosedBy, when: when}, nil
	}
	return nil, nil
}

// metaAt reads an issue's metadata as of one commit.
func (s *GitStore) metaAt(commit string) (*Issue, error) {
	out, err := s.git("show", commit+":meta.tony").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read meta.tony at %s: %w", shortSHA(commit), err)
	}
	node, err := parse.Parse(out)
	if err != nil {
		return nil, fmt.Errorf("failed to parse meta.tony at %s: %w", shortSHA(commit), err)
	}
	issue := &Issue{}
	if err := issue.FromTonyIR(node); err != nil {
		return nil, fmt.Errorf("failed to read meta.tony at %s: %w", shortSHA(commit), err)
	}
	// An older git-issue's merge can leave a key two values; a merge reads it
	// as GetByRef does, and the warning is GetByRef's to give.
	issue.Labels, _ = singleValued(issue.Labels)
	return issue, nil
}

func (s *GitStore) committerDate(commit string) (time.Time, error) {
	out, err := s.git("show", "-s", "--format=%cI", commit).Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to date %s: %w", shortSHA(commit), err)
	}
	when, err := time.Parse(time.RFC3339, strings.TrimSpace(string(out)))
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to read the date of %s: %w", shortSHA(commit), err)
	}
	return when, nil
}

// commitMerge writes the merged tree with the merged metadata in it, as a commit
// with both tips as parents -- so the next sync on either side is a
// fast-forward, and neither clone has to be told it lost.
func (s *GitStore) commitMerge(tree, meta, ours, theirs string) (string, error) {
	tmpIndex := tempIndexPath()
	defer os.Remove(tmpIndex)

	readTree := s.git("read-tree", tree)
	readTree.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	if err := readTree.Run(); err != nil {
		return "", fmt.Errorf("failed to read the merged tree: %w", err)
	}

	hash := s.git("hash-object", "-w", "--stdin")
	hash.Stdin = strings.NewReader(meta)
	hashOut, err := hash.Output()
	if err != nil {
		return "", fmt.Errorf("failed to hash the merged meta.tony: %w", err)
	}
	add := s.git("update-index", "--add", "--cacheinfo",
		"100644", strings.TrimSpace(string(hashOut)), "meta.tony")
	add.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	if err := add.Run(); err != nil {
		return "", fmt.Errorf("failed to place the merged meta.tony: %w", err)
	}

	write := s.git("write-tree")
	write.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	writeOut, err := write.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write the merged tree: %w", err)
	}

	message := fmt.Sprintf("merge: %s %s", shortSHA(ours), shortSHA(theirs))
	commit := s.git("commit-tree", strings.TrimSpace(string(writeOut)),
		"-p", ours, "-p", theirs, "-m", message)
	commitOut, err := commit.Output()
	if err != nil {
		return "", fmt.Errorf("failed to commit the merge: %w", err)
	}
	return strings.TrimSpace(string(commitOut)), nil
}
