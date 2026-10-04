package ops

import (
	"fmt"
	"sort"
	"strings"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// Edit rewrites what an issue says: its title, its body, or both. What is not
// given is kept, and an edit that changes nothing writes nothing.
//
// The edit is a commit on the issue's chain like every other write, and races
// as they do: when the ref moved between the read and the write -- a comment
// landed, a pull merged -- the store applies description.md again on the new
// tip (GitStore.Update), so what landed meanwhile at another path survives, and
// on description.md itself the later writer wins. What history keeps is every
// version, so nothing is lost to the chain.
//
// Changing what an issue says was an export, a hand-edit and an `import
// --force`, three commands and a flag whose name says "overwrite whatever is
// there" for a routine edit (dcp201s6h12krnmdpnn0).
func Edit(s issuelib.Store, id string, title, body *string) (*issuelib.Issue, error) {
	if title == nil && body == nil {
		return nil, fmt.Errorf("nothing to edit: give a title, a body, or both")
	}
	ref, err := own(s, id)
	if err != nil {
		return nil, err
	}
	issue, desc, err := s.GetByRef(ref)
	if err != nil {
		return nil, err
	}
	curTitle, curBody := SplitDescription(desc)
	newTitle, newBody := curTitle, curBody
	if title != nil {
		if newTitle = strings.TrimSpace(*title); newTitle == "" {
			return nil, fmt.Errorf("title cannot be empty")
		}
	}
	if body != nil {
		if newBody = strings.TrimSpace(*body); newBody == "" {
			return nil, fmt.Errorf("description cannot be empty")
		}
	}
	if newTitle == curTitle && newBody == curBody {
		return issue, nil
	}
	var what []string
	if newTitle != curTitle {
		what = append(what, "title")
	}
	if newBody != curBody {
		what = append(what, "body")
	}
	files := map[string]string{"description.md": Description(newTitle, newBody)}
	if err := s.Update(issue, "edit: "+strings.Join(what, ", "), files); err != nil {
		return nil, fmt.Errorf("failed to edit issue: %w", err)
	}
	issue.Title = newTitle
	return issue, nil
}

// Description is description.md from a title and a body: the title as the first
// line's heading, a blank line, the body.
func Description(title, body string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + strings.TrimSpace(body) + "\n"
}

// EditComment rewrites the text of one comment in place, and answers the path it
// is stored at. The comment is named as show prints it -- its path, with or
// without "discussion/" -- or by any unambiguous prefix of that; a name matching
// none or several is refused, the several listed.
//
// The comment keeps its path and its header, so it keeps its place in the
// discussion and its time. Its name was derived from the text it was made with
// and is not derived again: the name is the comment's identity, and something
// that refers to it by name still finds it. An edit that changes nothing writes
// nothing.
//
// As Edit is, the edit is a commit on the issue's chain, so history keeps what
// the comment said before, and it races as every write does. Two clones that
// edit one comment differently meet at the merge as two edits of the
// description do: as a conflict, for a person to decide.
func EditComment(s issuelib.Store, id, comment, text string) (*issuelib.Issue, string, error) {
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
	path, err := findComment(s, ref, comment)
	if err != nil {
		return nil, "", err
	}
	raw, err := s.ReadFile(ref, path)
	if err != nil {
		return nil, "", err
	}
	content := issuelib.ReplaceCommentText(string(raw), text)
	if content == string(raw) {
		return issue, path, nil
	}
	name := strings.TrimPrefix(path, "discussion/")
	if err := s.Update(issue, "edit: comment "+name, map[string]string{path: content}); err != nil {
		return nil, "", fmt.Errorf("failed to edit comment: %w", err)
	}
	return issue, path, nil
}

// FindComment reads one comment of an issue, named as EditComment takes it.
func FindComment(s issuelib.Store, id, comment string) (*Entry, error) {
	ref, err := s.FindRef(id)
	if err != nil {
		return nil, err
	}
	path, err := findComment(s, ref, comment)
	if err != nil {
		return nil, err
	}
	raw, err := s.ReadFile(ref, path)
	if err != nil {
		return nil, err
	}
	content := string(raw)
	when, ok := issuelib.ParseCommentTime(content)
	return &Entry{Path: path, When: when, HasTime: ok, Content: content,
		Text: issuelib.StripCommentHeader(content)}, nil
}

// findComment resolves a comment's name on the issue at ref to its path: the
// path itself, or the one comment whose path under discussion/ the name begins.
func findComment(s issuelib.Store, ref, name string) (string, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "discussion/")
	if name == "" {
		return "", fmt.Errorf("no comment named: give its name as show prints it")
	}
	var comments, attachments []string
	walkDiscussion(s, ref, "discussion", &comments, &attachments)
	var matches []string
	for _, path := range comments {
		rel := strings.TrimPrefix(path, "discussion/")
		if rel == name {
			return path, nil
		}
		if strings.HasPrefix(rel, name) {
			matches = append(matches, path)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no comment %q on this issue", name)
	case 1:
		return matches[0], nil
	}
	sort.Strings(matches)
	return "", fmt.Errorf("comment %q is ambiguous: %s", name, strings.Join(matches, ", "))
}
