// Package ops is what git-issue does to an issue, one function per operation,
// over a [issuelib.Store]. The CLI (commands) and the MCP server (mcpserver) are
// two front ends over it: each parses what it was given and calls the same
// function, so what "close" means -- the commit verified, the message written, the
// ref moved -- is decided once. An operation answers what it did as data; how that
// is shown is the front end's.
//
// Methods naming an issue take a full XIDR or any unambiguous prefix, as the store
// does. An operation refuses rather than guesses: an empty body, a label with no
// key, a close of a closed issue. The store's own refusals -- an issue that moved
// underneath a write, a merge it cannot make -- come through as they are.
package ops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// Create files an issue: the title is the first line of description.md, the body
// follows it. Both are required.
func Create(s issuelib.Store, title, body string) (*issuelib.Issue, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("description cannot be empty")
	}
	issue, err := s.Create(title, "# "+title+"\n\n"+strings.TrimSpace(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create issue: %w", err)
	}
	return issue, nil
}

// SplitDescription is description.md as a title and a body: the title is the first
// line with its "# " stripped, the body what follows, trimmed.
func SplitDescription(desc string) (title, body string) {
	first, rest, _ := strings.Cut(desc, "\n")
	return strings.TrimSpace(strings.TrimPrefix(first, "# ")), strings.TrimSpace(rest)
}

// Comment adds text to an issue's discussion, and answers the path it was stored
// at. The path is content-addressed (issuelib.CommentFileName), so two clones
// commenting at once never collide.
func Comment(s issuelib.Store, id, text string) (*issuelib.Issue, string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, "", fmt.Errorf("comment cannot be empty")
	}
	ref, err := own(s, id)
	if err != nil {
		return nil, "", err
	}
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	content := issuelib.CommentBody(now, text)
	path := issuelib.CommentFileName(now, content)

	firstLine := strings.Split(text, "\n")[0]
	if len(firstLine) > 60 {
		firstLine = firstLine[:57] + "..."
	}
	if err := s.Update(issue, "comment: "+firstLine, map[string]string{path: content}); err != nil {
		return nil, "", fmt.Errorf("failed to add comment: %w", err)
	}
	return issue, path, nil
}

// Label adds and removes labels, and answers the issue with the labels it has
// after. A label is normalized as the store spells it; key=value replaces the
// key's value; a bare key removes it whatever its value (issuelib.SetLabel,
// RemoveLabel). A label with no key is refused.
func Label(s issuelib.Store, id string, add, remove []string) (*issuelib.Issue, error) {
	norm := func(labels []string) ([]string, error) {
		out := make([]string, 0, len(labels))
		for _, l := range labels {
			l = issuelib.NormalizeLabel(l)
			if key, _, _ := issuelib.SplitLabel(l); key == "" {
				return nil, fmt.Errorf("%q has no key", l)
			}
			out = append(out, l)
		}
		return out, nil
	}
	add, err := norm(add)
	if err != nil {
		return nil, err
	}
	remove, err = norm(remove)
	if err != nil {
		return nil, err
	}
	if len(add) == 0 && len(remove) == 0 {
		return nil, fmt.Errorf("nothing to do: give labels to add or remove")
	}
	ref, err := own(s, id)
	if err != nil {
		return nil, err
	}
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		return nil, err
	}
	for _, l := range remove {
		issue.RemoveLabel(l)
	}
	for _, l := range add {
		issue.SetLabel(l)
	}
	if len(add) > 0 {
		sort.Strings(issue.Labels)
	}
	var what []string
	if len(add) > 0 {
		what = append(what, "added "+strings.Join(add, ", "))
	}
	if len(remove) > 0 {
		what = append(what, "removed "+strings.Join(remove, ", "))
	}
	if err := s.Update(issue, "label: "+strings.Join(what, "; "), nil); err != nil {
		return nil, fmt.Errorf("failed to update issue: %w", err)
	}
	return issue, nil
}

// Close closes an open issue, recording the commit that closed it when one is
// given. A closed issue is refused.
func Close(s issuelib.Store, id, commit string) (*issuelib.Issue, error) {
	var closedBy *string
	if commit != "" {
		sha, err := s.VerifyCommit(commit)
		if err != nil {
			return nil, err
		}
		closedBy = &sha
	}
	ref, err := own(s, id)
	if err != nil {
		return nil, err
	}
	if issuelib.IsClosedRef(ref) {
		return nil, fmt.Errorf("issue already closed: %s", id)
	}
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to read issue: %w", err)
	}
	issue.Status = "closed"
	issue.ClosedBy = closedBy
	msg := "close"
	if closedBy != nil {
		msg = fmt.Sprintf("close: closed by %s", (*closedBy)[:7])
	}
	if err := s.Update(issue, msg, nil); err != nil {
		return nil, fmt.Errorf("failed to update issue: %w", err)
	}
	// The ref moves from the open namespace to the closed one: the namespace is
	// the status.
	if err := s.MoveRef(ref, issuelib.ClosedRefForXIDR(issue.ID)); err != nil {
		return nil, fmt.Errorf("failed to move issue ref: %w", err)
	}
	return issue, nil
}

// Reopen reopens a closed issue. An open issue is refused.
func Reopen(s issuelib.Store, id string) (*issuelib.Issue, error) {
	ref, err := own(s, id)
	if err != nil {
		return nil, err
	}
	if !issuelib.IsClosedRef(ref) {
		return nil, fmt.Errorf("issue is not closed: %s", id)
	}
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		return nil, fmt.Errorf("failed to read issue: %w", err)
	}
	issue.Status = "open"
	issue.ClosedBy = nil
	if err := s.Update(issue, "reopen", nil); err != nil {
		return nil, fmt.Errorf("failed to update issue: %w", err)
	}
	if err := s.MoveRef(ref, issuelib.RefForXIDR(issue.ID)); err != nil {
		return nil, fmt.Errorf("failed to move issue ref: %w", err)
	}
	return issue, nil
}

// Link records a commit on an issue, and the issue on the commit (a git note, the
// reverse index). Answers the commit's full SHA.
func Link(s issuelib.Store, id, commit string) (*issuelib.Issue, string, error) {
	sha, err := s.VerifyCommit(commit)
	if err != nil {
		return nil, "", err
	}
	ref, err := own(s, id)
	if err != nil {
		return nil, "", err
	}
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read issue: %w", err)
	}
	if !issuelib.Contains(issue.Commits, sha) {
		issue.Commits = append(issue.Commits, sha)
		if err := s.Update(issue, "link: "+sha[:7], nil); err != nil {
			return nil, "", fmt.Errorf("failed to update issue: %w", err)
		}
	}
	// The note is the reverse index; a commit that cannot take one is still linked.
	_ = s.AddNote(sha, issue.ID)
	return issue, sha, nil
}

// Relation is how one issue stands to another.
type Relation string

const (
	Related   Relation = "related"
	Blocks    Relation = "blocks"
	Duplicate Relation = "duplicate"
)

// Relate records that issue id stands in relation kind to issue other, and
// answers the two issues and whether anything was written: a relation already
// recorded is not written again.
//
// blocks is held on both issues, as blocks on the first and blocked_by on the
// second, and the two are separate writes: the second can fail after the first
// landed. So the relationship is there only when both halves are, and a rerun
// writes whichever half is missing (addsgv1yh12kszdxmdn0).
func Relate(s issuelib.Store, id, other string, kind Relation) (from, to *issuelib.Issue, changed bool, err error) {
	switch kind {
	case Related, Blocks, Duplicate:
	default:
		return nil, nil, false, fmt.Errorf("unknown relation %q: related, blocks or duplicate", kind)
	}
	ref1, err := own(s, id)
	if err != nil {
		return nil, nil, false, err
	}
	ref2, err := s.FindRef(other)
	if err != nil {
		return nil, nil, false, err
	}
	from, _, err = s.GetByRef(ref1)
	if err != nil {
		return nil, nil, false, err
	}
	to, _, err = s.GetByRef(ref2)
	if err != nil {
		return nil, nil, false, err
	}

	var added bool
	var msg string
	switch kind {
	case Related:
		if !issuelib.Contains(from.RelatedIssues, to.ID) {
			from.RelatedIssues = append(from.RelatedIssues, to.ID)
			added = true
		}
		msg = "relate: link to " + issuelib.FormatID(to.ID)
	case Blocks:
		if !issuelib.Contains(from.Blocks, to.ID) {
			from.Blocks = append(from.Blocks, to.ID)
			added = true
		}
		msg = "blocks: " + issuelib.FormatID(to.ID)
	case Duplicate:
		if !issuelib.Contains(from.Duplicates, to.ID) {
			from.Duplicates = append(from.Duplicates, to.ID)
			added = true
		}
		msg = "duplicate: of " + issuelib.FormatID(to.ID)
	}
	// The far side of blocks is written on the other issue -- unless that issue
	// is a mirror, which is another repository's and not written here. The
	// relation is then this issue's alone, which is what this repository knows.
	addBlockedBy := kind == Blocks && !issuelib.IsExtRef(to.Ref) && !issuelib.Contains(to.BlockedBy, from.ID)
	if !added && !addBlockedBy {
		return from, to, false, nil
	}
	if added {
		if err := s.Update(from, msg, nil); err != nil {
			return nil, nil, false, fmt.Errorf("failed to update issue: %w", err)
		}
	}
	if addBlockedBy {
		to.BlockedBy = append(to.BlockedBy, from.ID)
		if err := s.Update(to, "blocked-by: "+issuelib.FormatID(from.ID), nil); err != nil {
			return nil, nil, false, fmt.Errorf("failed to record blocked_by on %s (rerun to complete it): %w",
				issuelib.FormatID(to.ID), err)
		}
	}
	return from, to, true, nil
}

// Linked is an issue named by another, or by a commit's note, as far as it can
// be read: Err says what stopped it, and the rest is empty then.
type Linked struct {
	ID     string
	Status string
	Title  string
	Err    string
}

// linked reads the issue an id names, into a Linked.
func linked(s issuelib.Store, id string) Linked {
	ref, err := s.FindRef(id)
	if err != nil {
		return Linked{ID: id, Err: "not found"}
	}
	issue, _, err := s.GetByRef(ref)
	if err != nil {
		return Linked{ID: id, Err: "error"}
	}
	return Linked{ID: issue.ID, Status: issuelib.StatusOf(issue), Title: issue.Title}
}

// ForCommit answers the issues linked to a commit, through its note, and the
// commit's full SHA.
func ForCommit(s issuelib.Store, commit string) (string, []Linked, error) {
	sha, err := s.VerifyCommit(commit)
	if err != nil {
		return "", nil, err
	}
	notes, err := s.GetNotes(sha)
	if err != nil {
		return sha, nil, nil // no note is no issues
	}
	var out []Linked
	for _, id := range strings.Split(notes, "\n") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, linked(s, id))
		}
	}
	return sha, out, nil
}

// List answers the open issues, or all of them, newest first, and only those
// carrying label when one is given.
func List(s issuelib.Store, all bool, label string) ([]*issuelib.Issue, error) {
	issues, err := s.List(all)
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}
	if label != "" {
		label = issuelib.NormalizeLabel(label)
		kept := issues[:0]
		for _, issue := range issues {
			if issuelib.Contains(issue.Labels, label) {
				kept = append(kept, issue)
			}
		}
		issues = kept
	}
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Created.After(issues[j].Created)
	})
	return issues, nil
}
