package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// An operation which calls out to the system is refused at the write. It used to
// commit, and then it RAN -- in the logd process, on every read, every replay and
// every snapshot build -- so the same commit read two ways and the store held a
// computation where a value was supposed to be (trqgmd1ah12kranxg5n0).
func TestUnsafeOperationIsRefusedAtTheWrite(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		refused          bool
	}{
		{name: "a pipe", path: "stamp", body: `!pipe "date +%s%N"`, refused: true},
		{name: "a pipe under a field", path: "", body: `{a: {b: !pipe "date"}}`, refused: true},
		{name: "a pipe in an array element", path: "", body: `{a: [1, !pipe "date"]}`, refused: true},
		// The escape stays open: a document which CONTAINS a patch is data, and a
		// store which refuses it cannot hold a charter or a rule.
		{name: "a pipe as data, escaped", path: "rule", body: `!raw {then: !pipe "date"}`},
		{name: "an ordinary write", path: "stamp", body: `hello`},
		{name: "a storable operation", path: "stamp", body: `!delete seed`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(t.TempDir(), nil)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()
			if _, err := arrayWriteCommit(t, s, "", `{stamp: seed}`); err != nil {
				t.Fatalf("seed: %v", err)
			}

			_, err = arrayWriteCommit(t, s, tc.path, tc.body)
			switch {
			case tc.refused && err == nil:
				t.Fatalf("%s committed; it executes", tc.body)
			case tc.refused:
				if !strings.Contains(err.Error(), "pipe") {
					t.Errorf("the refusal does not name the operation: %v", err)
				}
				t.Logf("refused: %v", err)
			case err != nil:
				t.Fatalf("%s was refused and executes nothing: %v", tc.body, err)
			}

			// Whatever happened, the store still reads, and reads the same twice.
			commit, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatalf("GetCurrentCommit: %v", err)
			}
			first := readWholeStore(t, s, commit)
			if second := readWholeStore(t, s, commit); first != second {
				t.Errorf("one commit, two documents:\n %s\n %s", first, second)
			}
		})
	}
}

// And nothing applies one either: RejectUnsafe is set where logd materializes
// state, so a patch which reached the log by any other route fails the read
// instead of running. mergeop has had the option since before logd stored
// anything; nothing set it.
func TestUnsafeOperationIsNotAppliedByNextState(t *testing.T) {
	doc, err := parse.Parse([]byte(`{stamp: bob}`))
	if err != nil {
		t.Fatalf("parse doc: %v", err)
	}
	patch, err := parse.Parse([]byte(`{stamp: !pipe "tr a-z A-Z"}`))
	if err != nil {
		t.Fatalf("parse patch: %v", err)
	}
	got, err := api.NextState(doc, patch)
	if err == nil {
		t.Fatalf("the pipe ran: %v", got)
	}
	if !strings.Contains(err.Error(), "unsafe") {
		t.Errorf("got %v, want a refusal naming the unsafe operation", err)
	}
}
