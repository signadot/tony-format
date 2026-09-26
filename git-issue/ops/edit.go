package ops

import (
	"fmt"
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
	ref, err := s.FindRef(id)
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
