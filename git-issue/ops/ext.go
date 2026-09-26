package ops

import (
	"fmt"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

// own resolves an id to a ref this repository may write: an issue of its own.
// A mirror is another repository's issue, read here and written there, and
// every write refuses it by name.
func own(s issuelib.Store, id string) (string, error) {
	ref, err := s.FindRef(id)
	if err != nil {
		return "", err
	}
	if source, xidr, ok := issuelib.ExtSource(ref); ok {
		return "", fmt.Errorf("%s is a mirror of %s's issue: it is edited there, not here", xidr, source)
	}
	return ref, nil
}

// SourceAdd records where another repository is, under a name mirrors from it
// carry.
func SourceAdd(s issuelib.Store, name, url string) error {
	return s.AddSource(name, url)
}

// Sources answers the repositories this one knows by name.
func Sources(s issuelib.Store) ([]issuelib.Source, error) {
	return s.Sources()
}

// Mirror fetches an issue from a source into this repository, read-only, and
// answers it. The id is the issue's full XIDR.
func Mirror(s issuelib.Store, source, xidr string) (*issuelib.Issue, error) {
	return s.Mirror(source, xidr)
}

// Refresh brings every mirror from a source up to the source, and answers how
// many.
func Refresh(s issuelib.Store, source string) (int, error) {
	return s.RefreshMirrors(source)
}

// Unmirror removes a mirror and, from every issue of this repository, the
// relations that named it -- each as a commit on that issue's chain -- and
// answers how many issues were touched.
func Unmirror(s issuelib.Store, id string) (int, error) {
	ref, err := s.FindRef(id)
	if err != nil {
		return 0, err
	}
	_, xidr, ok := issuelib.ExtSource(ref)
	if !ok {
		return 0, fmt.Errorf("%s is this repository's own issue, not a mirror", id)
	}
	touched := 0
	all, err := s.List(true)
	if err != nil {
		return 0, err
	}
	for _, issue := range all {
		if issuelib.IsExtRef(issue.Ref) {
			continue
		}
		changed := false
		for _, list := range []*[]string{&issue.RelatedIssues, &issue.Blocks, &issue.BlockedBy, &issue.Duplicates} {
			kept := (*list)[:0]
			for _, id := range *list {
				if id == xidr {
					changed = true
					continue
				}
				kept = append(kept, id)
			}
			*list = kept
		}
		if !changed {
			continue
		}
		if err := s.Update(issue, "unmirror: drop relation to "+issuelib.FormatID(xidr), nil); err != nil {
			return touched, fmt.Errorf("failed to drop the relation from %s: %w", issuelib.FormatID(issue.ID), err)
		}
		touched++
	}
	if err := s.Unmirror(ref); err != nil {
		return touched, err
	}
	return touched, nil
}
