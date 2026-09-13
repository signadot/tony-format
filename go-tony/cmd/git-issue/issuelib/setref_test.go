package issuelib

import (
	"strings"
	"testing"
)

// TestSetRefSaysWhatItExpected: a ref write names the tip it built on, and a
// create names no tip at all; either is refused when the ref disagrees.
func TestSetRefSaysWhatItExpected(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, err := s.Create("one", "# one\n")
	if err != nil {
		t.Fatal(err)
	}
	tip, err := s.GetRefCommit(issue.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.setRef(issue.Ref, tip, zeroSHA); err == nil {
		t.Error("a create over an existing ref was accepted")
	}
	if err := s.setRef(issue.Ref, tip, strings.Repeat("1", 40)); err == nil {
		t.Error("a write expecting the wrong tip was accepted")
	}
	if err := s.setRef(issue.Ref, tip, tip); err != nil {
		t.Errorf("a write expecting the tip it read was refused: %v", err)
	}
}
