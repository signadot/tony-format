package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// diff.go's diffArray has five gates before it will diff two arrays by identity -- a
// !key with exactly one arg on each side, the two args EQUAL, the arg parseable as a
// path, and DiffArrayByKey not erroring -- and every miss falls through to a POSITIONAL
// diff, silently.
//
// What that costs has CHANGED, and this test says what it is now rather than what it
// was. It used to be that a positional fall-through was an !arraydiff, !arraydiff is
// not in the storage vocabulary, and ValidateForStorage refused it -- so the miss could
// not be stored and the scope kept replaying, correct and slow.
//
// storableDelta answers absolutely, so there is no !arraydiff to refuse. A fall-through
// now states the array WHOLE, which is storable and, for baseline, correct: the array
// is what the write made it.
//
// For a SCOPE it is not correct, and the vocabulary is no longer what says so. Stating
// an array whole takes ownership of every element, so baseline can never add one again
// -- which is the failure scopeHasKeyedPaths exists to prevent, by refusing to write an
// overlay for a scope holding a keyed path the schema does not declare, and which
// lowerWrite prevents by not lowering such a write at all.
//
// So the guard moved upstream, from the vocabulary to the keyed checks, and it is worth
// knowing that absoluteness and identity-preservation are different properties: making
// every delta storable does not make every delta keep what a keyed array is for.
func TestDiffArrayGatesStateTheArrayWhole(t *testing.T) {
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
			// The builder's own step, which is now one: storableDelta, the same
			// call a write is lowered through and the overlay is built with.
			// Validation comes after it.
			d := storableDelta(from, to, nil, false)
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
			// Storable, and whole: no !arraydiff to refuse, and the array stated
			// entire rather than by position.
			if err != nil {
				t.Errorf("a fall-through is no longer unstorable, and was refused: %s\n delta: %s",
					err, text)
			}
			if strings.Contains(text, "!arraydiff") {
				t.Errorf("an absolute delta answered positionally: %s", text)
			}
			if !strings.Contains(text, "!insert") {
				t.Errorf("a fall-through should state the array whole: %s", text)
			}
		})
	}
}
