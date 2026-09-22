package commands

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

func label(t *testing.T, store issuelib.Store, remove bool, args ...string) {
	t.Helper()
	cc, _ := sayCC()
	if err := (&labelConfig{store: store, remove: remove}).run(cc, args); err != nil {
		t.Fatalf("label %v: %v", args, err)
	}
}

func labelsOf(t *testing.T, store issuelib.Store, id string) []string {
	t.Helper()
	issue, _, err := store.Get(id)
	if err != nil {
		t.Fatalf("get %s: %v", id, err)
	}
	return issue.Labels
}

// TestLabel_KeyValue: key=value replaces the key's value, a bare key unlabels
// whatever value it has, and a label with no key is refused.
func TestLabel_KeyValue(t *testing.T) {
	store := testRepo(t)
	issue, err := store.Create("Keyed", "# Keyed\n\nbody\n")
	if err != nil {
		t.Fatal(err)
	}

	label(t, store, false, issue.ID, "bug", "Severity=High")
	label(t, store, false, issue.ID, "severity=low")
	if got, want := labelsOf(t, store, issue.ID), []string{"bug", "severity=low"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("after setting a key again: %v, want %v", got, want)
	}

	label(t, store, true, issue.ID, "severity")
	if got, want := labelsOf(t, store, issue.ID), []string{"bug"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("after unlabelling the key: %v, want %v", got, want)
	}

	cc, _ := sayCC()
	if err := (&labelConfig{store: store}).run(cc, []string{issue.ID, "=x"}); err == nil {
		t.Error("a label with no key was accepted")
	}
}

// TestSync_RefusesAKeyMovedApartAndTakesOneMovedAlike: two clones move one
// issue's phase to different values. Neither is later in any sense that matters,
// so the pull names the key and leaves the issue; when this clone agrees with
// the other, the next pull merges.
func TestSync_RefusesAKeyMovedApartAndTakesOneMovedAlike(t *testing.T) {
	storeA, origin := pushTestRepo(t)
	issue, err := storeA.Create("Phased", "# Phased\n\nbody\n")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	label(t, storeA, false, issue.ID, "git-issue-phase=planned")
	pushA := newPushConfig(storeA)
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("push: %v", err)
	}

	b := secondClone(t, origin)
	inClone(t, b, func(storeB issuelib.Store) {
		if err := newPullConfig(storeB).run(pushCC(), []string{"origin"}); err != nil {
			t.Fatalf("B pull: %v", err)
		}
		label(t, storeB, false, issue.ID, "git-issue-phase=overtaken")
		comment(t, storeB, issue.ID, "from-b")
	})
	label(t, storeA, false, issue.ID, "git-issue-phase=landed")
	if err := pushA.pushAll(pushCC(), "origin"); err != nil {
		t.Fatalf("A push: %v", err)
	}

	inClone(t, b, func(storeB issuelib.Store) {
		before, _ := storeB.GetRefCommit(issuelib.RefForXIDR(issue.ID))
		cc, out := sayCC()
		if err := newPullConfig(storeB).run(cc, []string{"origin"}); err == nil {
			t.Error("a phase moved apart on both sides was answered as a success")
		}
		for _, want := range []string{issue.ID, "git-issue-phase", "landed", "overtaken"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("the refusal does not mention %q: %q", want, out.String())
			}
		}
		if at, _ := storeB.GetRefCommit(issuelib.RefForXIDR(issue.ID)); at != before {
			t.Errorf("the refused pull moved this clone to %s", shortSHA(at))
		}

		label(t, storeB, false, issue.ID, "git-issue-phase=landed")
		if err := newPullConfig(storeB).run(pushCC(), []string{"origin"}); err != nil {
			t.Fatalf("pull after agreeing: %v", err)
		}
		if got := labelsOf(t, storeB, issue.ID); strings.Join(got, ",") != "git-issue-phase=landed" {
			t.Errorf("after agreeing: %v, want one phase, landed", got)
		}
		files, err := storeB.ListDir(issuelib.RefForXIDR(issue.ID), "discussion")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := files["from-b.md"]; !ok {
			t.Errorf("B's comment did not survive the merge: %v", files)
		}
	})
}
