package storage

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// diff.go's diffArray has five gates before it will diff two arrays by identity --
// a !key with exactly one arg on each side, the two args EQUAL, the arg parseable as
// a path, and DiffArrayByKey not erroring -- and every miss falls through to a
// POSITIONAL diff, silently. The overlay builder annotates both states from the
// schema and then relies on that branch being taken, so the plan (§7) called the
// silence a correctness hazard: a delta that merges by position lands on whatever is
// at that index once baseline has moved.
//
// It is not, and this says why. A positional diff is an !arraydiff, and !arraydiff is
// not in the storage vocabulary -- deliberately, and for exactly this reason: "relative
// to the array that was there, and positional". WriteScopeOverlay validates before it
// appends, so a fall-through cannot be stored. writeScopeOverlays logs the refusal and
// the scope keeps replaying, which is correct and slow rather than fast and wrong.
//
// The silence is therefore right where it is. For an ordinary Diff a positional
// fall-through IS the correct answer -- it round trips, it is merely less
// identity-preserving -- and only the storage path needs more, which the storage path
// asks for itself.
//
// This holds while overlays are the only lowered artefact. When lowering reaches
// baseline writes, they have to be held to the same vocabulary or this stops being
// true there (checkStorableInScope exempts baseline today, deliberately).
func TestDiffArrayGatesCannotReachStorage(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		storable       bool
	}{{
		name: "gate 1: from carries no !key",
		from: `{items: [{sku: "A", q: 1}]}`,
		to:   `{items: !key(sku) [{sku: "A", q: 2}]}`,
	}, {
		name: "gate 2: to carries no !key",
		from: `{items: !key(sku) [{sku: "A", q: 1}]}`,
		to:   `{items: [{sku: "A", q: 2}]}`,
	}, {
		name: "gate 3: the two sides are keyed by different fields",
		from: `{items: !key(sku) [{sku: "A", q: 1}]}`,
		to:   `{items: !key(id) [{sku: "A", q: 2}]}`,
	}, {
		name: "gate 4: the arg does not parse as a path",
		from: `{items: !key("[") [{sku: "A", q: 1}]}`,
		to:   `{items: !key("[") [{sku: "A", q: 2}]}`,
	}, {
		// What the builder is aiming for, and what it gets when the annotation
		// did its job: an identity merge, storable.
		name:     "both sides keyed the same, which is the annotated case",
		from:     `{items: !key(sku) [{sku: "A", q: 1}]}`,
		to:       `{items: !key(sku) [{sku: "A", q: 2}]}`,
		storable: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			from, err := parse.Parse([]byte(tc.from))
			if err != nil {
				t.Fatalf("from: %s", err)
			}
			to, err := parse.Parse([]byte(tc.to))
			if err != nil {
				t.Fatalf("to: %s", err)
			}
			// The overlay builder's own steps: diff, then lower the checked
			// primitives away. Validation comes after both.
			d := unconditionalPatch(tony.Diff(from.Clone(), to.Clone()))
			if d == nil {
				t.Fatal("no diff between two states that differ")
			}
			text := strings.Join(strings.Fields(encode.MustString(d)), " ")
			err = api.ValidateForStorage(d)
			if tc.storable {
				if err != nil {
					t.Errorf("the keyed diff is not storable: %s\n delta: %s", err, text)
				}
				return
			}
			if err == nil {
				t.Fatalf("a positional fall-through was accepted for storage\n delta: %s", text)
			}
			// The refusal has to name the op, or an operator reading the log
			// cannot tell this from any other unstorable thing.
			if !strings.Contains(err.Error(), "!arraydiff") {
				t.Errorf("refused with %q, which does not name !arraydiff\n delta: %s", err, text)
			}
		})
	}
}
