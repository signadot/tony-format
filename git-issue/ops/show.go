package ops

import (
	"sort"
	"strings"
	"time"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// Shown is everything about one issue: what a person reads with `show`, and what
// an agent reads as data.
type Shown struct {
	Issue       *issuelib.Issue
	Ref         string
	Status      string
	Description string // description.md as stored: title line and body
	Title       string
	Body        string
	Source      string // for a mirror, the source it is mirrored from; "" for this repository's own

	Commits    []string // one line per linked commit, as git shows it
	Related    []Linked
	Blocks     []Linked
	BlockedBy  []Linked
	Duplicates []Linked

	Comments    []Entry  // in chronological order
	Attachments []string // paths under discussion/files/
}

// Entry is one comment in an issue's discussion.
type Entry struct {
	Path    string
	When    time.Time // from the comment's header; zero when it has none
	HasTime bool
	Content string // as stored, header included
	Text    string // the content without its header
}

// Show reads an issue whole.
func Show(s issuelib.Store, id string) (*Shown, error) {
	ref, err := s.FindRef(id)
	if err != nil {
		return nil, err
	}
	issue, desc, err := s.GetByRef(ref)
	if err != nil {
		return nil, err
	}
	sh := &Shown{
		Issue:       issue,
		Ref:         ref,
		Status:      issuelib.StatusOf(issue),
		Description: desc,
	}
	sh.Title, sh.Body = SplitDescription(desc)
	sh.Source, _, _ = issuelib.ExtSource(ref)
	for _, c := range issue.Commits {
		info, err := s.GetCommitInfo(c)
		if err != nil {
			info = c
		}
		sh.Commits = append(sh.Commits, info)
	}
	each := func(ids []string) []Linked {
		var out []Linked
		for _, id := range ids {
			out = append(out, linked(s, id))
		}
		return out
	}
	sh.Related = each(issue.RelatedIssues)
	sh.Blocks = each(issue.Blocks)
	sh.BlockedBy = each(issue.BlockedBy)
	sh.Duplicates = each(issue.Duplicates)

	var comments []string
	walkDiscussion(s, ref, "discussion", &comments, &sh.Attachments)
	for _, path := range comments {
		raw, err := s.ReadFile(ref, path)
		if err != nil {
			continue
		}
		content := string(raw)
		when, ok := issuelib.ParseCommentTime(content)
		sh.Comments = append(sh.Comments, Entry{
			Path: path, When: when, HasTime: ok,
			Content: content, Text: issuelib.StripCommentHeader(content),
		})
	}
	// Chronological by each comment's own timestamp, the path breaking ties: a
	// current name (<UTC ts>-<hash>) sorts by time too, but a legacy
	// discussion/NNN.md carries a count, and only its header says when.
	sort.SliceStable(sh.Comments, func(i, j int) bool {
		a, b := sh.Comments[i], sh.Comments[j]
		if a.HasTime && b.HasTime && !a.When.Equal(b.When) {
			return a.When.Before(b.When)
		}
		return a.Path < b.Path
	})
	sort.Strings(sh.Attachments)
	return sh, nil
}

// walkDiscussion splits the discussion subtree into comments and attachments: an
// .md outside discussion/files/ is a comment, anything else an attachment. A
// missing subtree is no discussion.
func walkDiscussion(s issuelib.Store, ref, dir string, comments, attachments *[]string) {
	entries, err := s.ListDir(ref, dir)
	if err != nil {
		return
	}
	for name, entry := range entries {
		full := dir + "/" + name
		switch {
		case strings.HasPrefix(entry, "tree:"):
			walkDiscussion(s, ref, full, comments, attachments)
		case issuelib.IsCommentFile(full):
			*comments = append(*comments, full)
		default:
			*attachments = append(*attachments, full)
		}
	}
}
