package ops

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/git-issue/issuelib"
)

func str(s string) *string { return &s }

// TestEdit: the title alone, the body alone, both, and what is refused. Each
// edit is a commit on the chain, so what the issue said before is history.
func TestEdit(t *testing.T) {
	s := testRepo(t)
	issue, err := Create(s, "First title", "First body.")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetRefCommit(issue.Ref)

	if _, err := Edit(s, issue.ID, nil, nil); err == nil {
		t.Error("an edit of nothing was accepted")
	}
	if _, err := Edit(s, issue.ID, str(" "), nil); err == nil {
		t.Error("an empty title was accepted")
	}
	if _, err := Edit(s, issue.ID, nil, str("\n")); err == nil {
		t.Error("an empty body was accepted")
	}
	if at, _ := s.GetRefCommit(issue.Ref); at != before {
		t.Fatal("a refused edit moved the ref")
	}

	if _, err := Edit(s, issue.ID[:4], str("Second title"), nil); err != nil {
		t.Fatal(err)
	}
	sh, _ := Show(s, issue.ID)
	if sh.Title != "Second title" || sh.Body != "First body." {
		t.Errorf("after editing the title: %q / %q", sh.Title, sh.Body)
	}
	if _, err := Edit(s, issue.ID, nil, str("Second body.\n\n## With a heading\n")); err != nil {
		t.Fatal(err)
	}
	sh, _ = Show(s, issue.ID)
	if sh.Title != "Second title" || sh.Body != "Second body.\n\n## With a heading" {
		t.Errorf("after editing the body: %q / %q", sh.Title, sh.Body)
	}
	if _, err := Edit(s, issue.ID, str("Third"), str("Third.")); err != nil {
		t.Fatal(err)
	}
	sh, _ = Show(s, issue.ID)
	if sh.Description != "# Third\n\nThird.\n" {
		t.Errorf("after editing both: %q", sh.Description)
	}
	if listed, _ := List(s, false, ""); len(listed) != 1 || listed[0].Title != "Third" {
		t.Errorf("the listing reads the title as %q", listed[0].Title)
	}

	// An edit that changes nothing writes nothing.
	at, _ := s.GetRefCommit(issue.Ref)
	if _, err := Edit(s, issue.ID, str("Third"), str("Third.")); err != nil {
		t.Fatal(err)
	}
	if now, _ := s.GetRefCommit(issue.Ref); now != at {
		t.Error("an edit that changed nothing moved the ref")
	}
}

// staleReads is a store whose read of an issue is followed, before the caller
// sees it, by a comment landing on the real store: what a read races against.
type staleReads struct {
	issuelib.Store
	t *testing.T
}

func (s staleReads) GetByRef(ref string) (*issuelib.Issue, string, error) {
	issue, desc, err := s.Store.GetByRef(ref)
	if err != nil {
		return nil, "", err
	}
	if _, _, err := Comment(s.Store, issue.ID, "landed meanwhile"); err != nil {
		s.t.Fatal(err)
	}
	return issue, desc, nil
}

// TestEditRacingAComment: an edit built on a read the chain has moved past is
// applied on the new tip, as every write is, so the comment that landed between
// the read and the write is still there afterwards, and so is the edit.
func TestEditRacingAComment(t *testing.T) {
	real := testRepo(t)
	issue, err := Create(real, "Title", "Body.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Edit(staleReads{Store: real, t: t}, issue.ID, nil, str("Edited body.")); err != nil {
		t.Fatalf("the edit was refused: %v", err)
	}
	sh, err := Show(real, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Body != "Edited body." {
		t.Errorf("the edit did not land: %q", sh.Body)
	}
	if len(sh.Comments) != 1 || !strings.Contains(sh.Comments[0].Text, "landed meanwhile") {
		t.Errorf("the comment that landed meanwhile is gone: %+v", sh.Comments)
	}
}
