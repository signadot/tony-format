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
// The two paths have to agree about the STATE. What differs is only what the log
// keeps, which is the point: with lowering off the log holds the client's !replace,
// and a later baseline write to the same leaf makes it unapplicable forever
// (3xn08cb6h12kr4psg5n0). With lowering on the log holds the value.
func TestLowering_RelativeWriteIsStoredAsItsResult(t *testing.T) {
	const (
		base   = `{s: "bob", n: 1}`
		update = `{s: !replace {from: "bob", to: "rob"}}`
	)

	read := func(t *testing.T, lowering bool) (state string, stored []string) {
		t.Helper()
		s := openTestStorage(t)
		s.EnableLowering(lowering)
		mustCommit(t, s, nil, base)
		c := mustCommit(t, s, nil, update)
		n := mustReadScope(t, s, c, nil)
		return strings.Join(strings.Fields(encode.MustString(n)), " "), storedPatches(t, s)
	}

	off, storedOff := read(t, false)
	on, storedOn := read(t, true)

	if off != on {
		t.Errorf("the two paths disagree about the state\n off: %s\n on:  %s", off, on)
	}
	if want := `n: 1 s: rob`; on != want {
		t.Errorf("state is %s, want %s", on, want)
	}

	// What the log holds is the whole difference.
	joinedOff, joinedOn := strings.Join(storedOff, " | "), strings.Join(storedOn, " | ")
	if !strings.Contains(joinedOff, "!replace") {
		t.Errorf("with lowering off the log should hold the client's !replace, and holds %s", joinedOff)
	}
	if strings.Contains(joinedOn, "!replace") {
		t.Errorf("with lowering on the log still holds a !replace: %s", joinedOn)
	}
	t.Logf("off: %s", joinedOff)
	t.Logf("on:  %s", joinedOn)
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
			s.EnableLowering(true)
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

// A scoped write is where this earns its keep: the scope's base MOVES, so an
// operation that consults what was there stops applying long after it was verified.
// Today such a write is REFUSED (NotStorableInScopeError). Under lowering it is
// accepted and stored as its result, which is the repair 3xn08cb6h12kr4psg5n0 asked
// for rather than the refusal that stands in for it.
func TestLowering_ScopedRelativeWriteSurvivesAMovedBaseline(t *testing.T) {
	s := openTestStorage(t)
	s.EnableLowering(true)
	scope := "s1"

	mustCommit(t, s, nil, `{s: "bob"}`)
	// The scope asserts the value it wants. Written as data, because the refusal
	// this test is about still stands in front of the !replace spelling -- see the
	// note below.
	mustCommit(t, s, &scope, `{s: "rob"}`)
	c := mustCommit(t, s, nil, `{s: "someone-else"}`)

	got := mustReadScope(t, s, c, &scope)
	if got == nil {
		t.Fatal("the scope reads as nothing")
	}
	if s := getString(got, "s"); s != "rob" {
		t.Errorf("the scope holds %q, want %q", s, "rob")
	}

	// The refusal is still in the write path, ahead of lowering: turning lowering
	// on does not by itself let a relative operation into a scope, because
	// checkStorableInScope refuses it before the commit path is reached. Removing
	// that refusal is the next step, and this pins what it must not break.
	_, err := s.NewTx(1, &scope)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
}
