package issuelib

import (
	"errors"
	"strings"
	"testing"
)

// TestSetLabel_AKeyHoldsOneValue: setting a key replaces the value it had, and
// leaves plain labels and other keys alone.
func TestSetLabel_AKeyHoldsOneValue(t *testing.T) {
	i := &Issue{Labels: []string{"bug", "git-issue-phase=planned", "severity=high"}}
	i.SetLabel("git-issue-phase=landed")
	i.SetLabel("bug")
	if want := []string{"bug", "severity=high", "git-issue-phase=landed"}; !equalStrings(i.Labels, want) {
		t.Errorf("labels %v, want %v", i.Labels, want)
	}
}

// TestRemoveLabel_ABareKeyRemovesAnyValue: a bare key removes the key whatever
// its value, and a plain label of that name; key=value removes only that value.
func TestRemoveLabel_ABareKeyRemovesAnyValue(t *testing.T) {
	i := &Issue{Labels: []string{"phase", "phase=planned", "phases=x", "bug"}}
	i.RemoveLabel("phase")
	if want := []string{"phases=x", "bug"}; !equalStrings(i.Labels, want) {
		t.Errorf("bare key: labels %v, want %v", i.Labels, want)
	}

	i = &Issue{Labels: []string{"phase=planned"}}
	i.RemoveLabel("phase=landed")
	if want := []string{"phase=planned"}; !equalStrings(i.Labels, want) {
		t.Errorf("another value: labels %v, want %v", i.Labels, want)
	}
	i.RemoveLabel("phase=planned")
	if len(i.Labels) != 0 {
		t.Errorf("its value: labels %v, want none", i.Labels)
	}
}

// TestSingleValued_KeepsTheLastValue: the shape an older client's union merge
// leaves. The last value listed is kept where it stands, and every value is
// reported.
func TestSingleValued_KeepsTheLastValue(t *testing.T) {
	got, multi := singleValued([]string{"phase=planned", "bug", "phase=landed", "k=v"})
	if want := []string{"bug", "phase=landed", "k=v"}; !equalStrings(got, want) {
		t.Errorf("labels %v, want %v", got, want)
	}
	if want := []string{"phase=planned", "phase=landed"}; !equalStrings(multi["phase"], want) {
		t.Errorf("reported %v, want %v", multi["phase"], want)
	}
	if len(multi) != 1 {
		t.Errorf("reported keys %v, want only phase", multi)
	}

	in := []string{"bug", "k=v"}
	if got, multi := singleValued(in); !equalStrings(got, in) || multi != nil {
		t.Errorf("a list in shape came back %v, %v", got, multi)
	}
}

// TestMergeSet_KeepsEachSidesRemovals is the defect: a union undid a removal
// whenever the other side had changed anything.
func TestMergeSet_KeepsEachSidesRemovals(t *testing.T) {
	for _, c := range []struct {
		name               string
		base, ours, theirs []string
		want               []string
	}{
		{"removed here", []string{"a", "b"}, []string{"a"}, []string{"a", "b"}, []string{"a"}},
		{"removed there", []string{"a", "b"}, []string{"a", "b"}, []string{"b"}, []string{"b"}},
		{"added on both", []string{}, []string{"a", "b"}, []string{"b", "c"}, []string{"a", "b", "c"}},
		{"removed on both", []string{"a"}, []string{}, []string{}, []string{}},
		{"removed and re-added", []string{"a"}, []string{}, []string{"a", "b"}, []string{"b"}},
	} {
		if got := mergeSet(c.base, c.ours, c.theirs); !equalStrings(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestMergeLabels_DecidesAKeyByWhoChangedIt: a side that changed a key is
// taken, both changing it alike agree, and both changing it differently is two
// people moving one key at once, which is refused.
func TestMergeLabels_DecidesAKeyByWhoChangedIt(t *testing.T) {
	const planned, landed, overtaken = "git-issue-phase=planned", "git-issue-phase=landed", "git-issue-phase=overtaken"
	for _, c := range []struct {
		name               string
		base, ours, theirs []string
		want               []string
	}{
		{"moved here", []string{planned}, []string{landed}, []string{planned, "bug"}, []string{landed, "bug"}},
		{"moved there", []string{planned}, []string{planned}, []string{landed}, []string{landed}},
		{"moved alike", []string{planned}, []string{landed}, []string{landed}, []string{landed}},
		{"removed there", []string{planned, "bug"}, []string{planned, "bug"}, []string{"bug"}, []string{"bug"}},
		{"set on one side", nil, []string{"bug"}, []string{planned}, []string{"bug", planned}},
	} {
		got, err := mergeLabels(c.base, c.ours, c.theirs)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !equalStrings(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}

	for _, c := range []struct {
		name               string
		base, ours, theirs []string
	}{
		{"moved apart", []string{planned}, []string{landed}, []string{overtaken}},
		{"moved here, removed there", []string{planned}, []string{landed}, nil},
		{"set apart", nil, []string{landed}, []string{overtaken}},
	} {
		_, err := mergeLabels(c.base, c.ours, c.theirs)
		if !errors.Is(err, ErrDiverged) {
			t.Errorf("%s: %v, want a divergence", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), "git-issue-phase") {
			t.Errorf("%s: %q does not name the key", c.name, err)
		}
	}
}

// TestMergeIssue_ARemovalSurvivesAnEditElsewhere: one clone moves a phase and
// drops a label while the other only comments. Before, the merge put both old
// labels back, and the issue had two phases after a race that never happened.
func TestMergeIssue_ARemovalSurvivesAnEditElsewhere(t *testing.T) {
	gitInit(t)
	s := NewGitStoreWithOutput(&strings.Builder{})
	issue, _, theirs := forkSides(t, s)
	edit(t, s, issue.Ref, "", func(i *Issue) { i.Labels = []string{"git-issue-phase=planned", "wip"} })
	putRef(t, theirs, refAtOrEmpty(t, issue.Ref))
	base := refAtOrEmpty(t, issue.Ref)

	edit(t, s, issue.Ref, "", func(i *Issue) {
		i.SetLabel("git-issue-phase=landed")
		i.RemoveLabel("wip")
	})
	theirIssue, _, err := s.GetByRef(theirs)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(theirIssue, "comment: elsewhere",
		map[string]string{"discussion/elsewhere.md": "elsewhere\n"}); err != nil {
		t.Fatal(err)
	}

	merged, err := s.MergeIssue(base, refAtOrEmpty(t, issue.Ref), refAtOrEmpty(t, theirs))
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, err := s.metaAt(merged)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"git-issue-phase=landed"}; !equalStrings(got.Labels, want) {
		t.Errorf("labels %v, want %v", got.Labels, want)
	}
}

// TestGetByRef_WarnsOfAKeyWithTwoValues: an older client's merge can leave a
// key two values. The read takes the last, and says so on the store's warning
// writer with what a person would type to fix it.
func TestGetByRef_WarnsOfAKeyWithTwoValues(t *testing.T) {
	gitInit(t)
	var warned strings.Builder
	s := NewGitStoreWithOutput(&warned)
	issue, err := s.Create("subject", "# subject\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	// Written as an older client's merge leaves it: Update does not normalize.
	issue.Labels = []string{"git-issue-phase=planned", "git-issue-phase=landed"}
	if err := s.Update(issue, "merge: as an older client", nil); err != nil {
		t.Fatal(err)
	}
	warned.Reset()

	got, _, err := s.GetByRef(issue.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"git-issue-phase=landed"}; !equalStrings(got.Labels, want) {
		t.Errorf("labels %v, want %v", got.Labels, want)
	}
	for _, want := range []string{FormatID(issue.ID), "git-issue-phase", "planned", "landed", "git issue label"} {
		if !strings.Contains(warned.String(), want) {
			t.Errorf("the warning does not mention %q: %q", want, warned.String())
		}
	}
}
