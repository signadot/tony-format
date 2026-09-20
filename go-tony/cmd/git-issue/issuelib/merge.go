package issuelib

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
// <timestamp>-<hash> and cannot collide, and meta.tony's lists are sets. What
// needs deciding is the status, and the rule for it is below.
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
	out, err := exec.Command("git", "merge-base", ours, theirs).Output()
	if err != nil {
		return "", fmt.Errorf("%s and %s share no history", shortSHA(ours), shortSHA(theirs))
	}
	return strings.TrimSpace(string(out)), nil
}

// mergeTrees merges the two trees and answers the merged one. A conflict in
// meta.tony is not one: it is replaced wholesale by mergeMeta, and git reaching
// for text markers in a generated file says nothing about the issue.
func (s *GitStore) mergeTrees(base, ours, theirs string) (string, error) {
	cmd := exec.Command("git", "merge-tree", "--write-tree", "--merge-base="+base, ours, theirs)
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
// The lists are sets, and union: what two clones recorded about one issue is
// both of those things. id and created are the base's, since neither side may
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
	merged.Commits = mergeList(ourIssue.Commits, theirIssue.Commits)
	merged.Branches = mergeList(ourIssue.Branches, theirIssue.Branches)
	merged.Labels = mergeList(ourIssue.Labels, theirIssue.Labels)
	merged.RelatedIssues = mergeList(ourIssue.RelatedIssues, theirIssue.RelatedIssues)
	merged.Blocks = mergeList(ourIssue.Blocks, theirIssue.Blocks)
	merged.BlockedBy = mergeList(ourIssue.BlockedBy, theirIssue.BlockedBy)
	merged.Duplicates = mergeList(ourIssue.Duplicates, theirIssue.Duplicates)

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

// mergeList unions two of an issue's lists, keeping ours in order and adding
// what only theirs has.
func mergeList(ours, theirs []string) []string {
	out := append([]string{}, ours...)
	for _, item := range theirs {
		if !Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
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
	out, err := exec.Command("git", "rev-list", base+".."+tip).Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read the history of %s: %w", shortSHA(tip), err)
	}
	for _, commit := range strings.Fields(strings.TrimSpace(string(out))) {
		issue, err := s.metaAt(commit)
		if err != nil {
			continue // a commit whose metadata cannot be read says nothing
		}
		parents, err := exec.Command("git", "rev-parse", commit+"^1").Output()
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
	out, err := exec.Command("git", "show", commit+":meta.tony").Output()
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
	return issue, nil
}

func (s *GitStore) committerDate(commit string) (time.Time, error) {
	out, err := exec.Command("git", "show", "-s", "--format=%cI", commit).Output()
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

	readTree := exec.Command("git", "read-tree", tree)
	readTree.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	if err := readTree.Run(); err != nil {
		return "", fmt.Errorf("failed to read the merged tree: %w", err)
	}

	hash := exec.Command("git", "hash-object", "-w", "--stdin")
	hash.Stdin = strings.NewReader(meta)
	hashOut, err := hash.Output()
	if err != nil {
		return "", fmt.Errorf("failed to hash the merged meta.tony: %w", err)
	}
	add := exec.Command("git", "update-index", "--add", "--cacheinfo",
		"100644", strings.TrimSpace(string(hashOut)), "meta.tony")
	add.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	if err := add.Run(); err != nil {
		return "", fmt.Errorf("failed to place the merged meta.tony: %w", err)
	}

	write := exec.Command("git", "write-tree")
	write.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	writeOut, err := write.Output()
	if err != nil {
		return "", fmt.Errorf("failed to write the merged tree: %w", err)
	}

	message := fmt.Sprintf("merge: %s %s", shortSHA(ours), shortSHA(theirs))
	commit := exec.Command("git", "commit-tree", strings.TrimSpace(string(writeOut)),
		"-p", ours, "-p", theirs, "-m", message)
	commitOut, err := commit.Output()
	if err != nil {
		return "", fmt.Errorf("failed to commit the merge: %w", err)
	}
	return strings.TrimSpace(string(commitOut)), nil
}
