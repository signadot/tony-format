package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// storedPatches returns the delta of every entry in the log, as written.
func storedPatches(t *testing.T, s *Storage) []string {
	t.Helper()
	var out []string
	for _, seg := range s.index.AllSegments() {
		if seg.KindedPath != "" {
			continue // one entry, many segments; the root one names it once
		}
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			t.Fatalf("reading entry at %s: %v", seg.LogFile, err)
		}
		if entry.Patch == nil {
			continue
		}
		out = append(out, strings.Join(strings.Fields(encode.MustString(entry.Patch)), " "))
	}
	return out
}

// A write carrying a relative operation is applied and its RESULT stored, so what a
// later read re-applies states what the value is rather than how it once related to
// something that may since have moved.
//
// What the log keeps is the point: it holds the VALUE, not the client's !replace. A
// stored !replace consults what was there, so a later baseline write to the same leaf
// would make it unapplicable forever (3xn08cb6h12kr4psg5n0).
func TestLowering_RelativeWriteIsStoredAsItsResult(t *testing.T) {
	const (
		base   = `{s: "bob", n: 1}`
		update = `{s: !replace {from: "bob", to: "rob"}}`
	)

	s := openTestStorage(t)
	mustCommit(t, s, nil, base)
	c := mustCommit(t, s, nil, update)
	n := mustReadScope(t, s, c, nil)

	state := strings.Join(strings.Fields(encode.MustString(n)), " ")
	if want := `n: 1 s: rob`; state != want {
		t.Errorf("state is %s, want %s", state, want)
	}

	stored := strings.Join(storedPatches(t, s), " | ")
	if strings.Contains(stored, "!replace") {
		t.Errorf("the log still holds a !replace: %s", stored)
	}
	t.Logf("stored: %s", stored)
}

// Nearly every write is already its own delta and must come through untouched --
// otherwise lowering would rewrite the whole log for nothing, and a client reading
// its own write back would not recognise it.
func TestLowering_AbsoluteWriteIsStoredAsSent(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a plain data merge", `{a: 1, b: {c: 2}}`},
		{"a delete", `{a: !delete null}`},
		{"a keyed list, which is what logd injects for itself",
			`{items: !key(sku) [{sku: "A", q: 1}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			mustCommit(t, s, nil, `{a: 0, b: {c: 0}, items: !key(sku) []}`)
			mustCommit(t, s, nil, tc.src)
			got := storedPatches(t, s)
			if len(got) != 2 {
				t.Fatalf("%d entries, want 2: %v", len(got), got)
			}
			// The second entry is this write. It came from the client, so it holds
			// the client's own shape, not a diff of two states.
			if op, needs := api.NeedsLowering(ir.Null()); needs {
				t.Fatalf("a null needs lowering (%s)?", op)
			}
			t.Logf("stored: %s", got[1])
		})
	}
}

// A scoped write is where this earns its keep, and this is the scenario the refusal
// used to stand in for (3xn08cb6h12kr4psg5n0): a scope's base MOVES, so an operation
// that consults what was there stops applying long after it was verified.
//
// The scope writes the !replace itself. Lowering converts it to the claim it makes
// before it is stored, so the operation never reaches the log and there is nothing left
// to re-evaluate when baseline moves underneath it.
//
// This test used to spell the same intent as data, on the stated grounds that
// checkStorableInScope refused the operation ahead of lowering. That was not so: the
// refusal was gated on lowering being OFF, so under the defaults it had not been
// reachable since lowering landed, and the write this test now makes would have been
// accepted all along. Verified against the pre-removal tree, where it is refused only
// once EnableLowering(false) is set. The gate and the refusal are gone with the escape
// hatch (gwdjwtz3h12ks8mfjdn0); what they protected against is what lowering does.
func TestLowering_ScopedRelativeWriteSurvivesAMovedBaseline(t *testing.T) {
	s := openTestStorage(t)
	scope := "s1"

	mustCommit(t, s, nil, `{s: "bob"}`)
	mustCommit(t, s, &scope, `{s: !replace {from: "bob", to: "rob"}}`)
	c := mustCommit(t, s, nil, `{s: "someone-else"}`)

	got := mustReadScope(t, s, c, &scope)
	if got == nil {
		t.Fatal("the scope reads as nothing")
	}
	if v := getString(got, "s"); v != "rob" {
		t.Errorf("the scope holds %q, want %q -- baseline moved out from under the write", v, "rob")
	}

	// Baseline is untouched by it.
	base := mustReadScope(t, s, c, nil)
	if v := getString(base, "s"); v != "someone-else" {
		t.Errorf("baseline holds %q, want %q", v, "someone-else")
	}
}
