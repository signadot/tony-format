package parse

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
)

func roundTripComments(t *testing.T, src string) (string, error) {
	t.Helper()
	n, err := Parse([]byte(src), ParseComments(true))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := encode.Encode(n, &buf, encode.EncodeFormat(format.TonyFormat),
		encode.EncodeComments(true)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// A comment as the first thing inside a brace object used to fail to parse: parseObj only
// collected comments once it had seen a pair, so a leading one fell through to the arm
// that rejects unexpected tokens. The same comment one line lower always worked.
func TestLeadingCommentInBraceObject(t *testing.T) {
	srcs := []string{
		"g: {\n  # c\n  a: 1\n}\n",
		"g: {\n  # one\n  # two\n  a: 1\n}\n",
		"hello:\n  g: {\n    # this\n    # is\n    42\n  }\n",
		"g: {\n  # c\n  a: 1\n  b: 2\n}\n",
	}
	for _, src := range srcs {
		if _, err := Parse([]byte(src), ParseComments(true)); err != nil {
			t.Errorf("Parse(%q) error = %v", src, err)
		}
		// Also with comment collection off, which takes the same path.
		if _, err := Parse([]byte(src)); err != nil {
			t.Errorf("Parse(%q) (no comments) error = %v", src, err)
		}
	}
}

// Comments between object keys were parsed into a map that objFromKVs accepted and never
// read, so they were silently dropped on a round trip — in brace and indented objects and
// at the top level alike, while arrays kept theirs.
func TestInteriorObjectCommentsSurvive(t *testing.T) {
	srcs := map[string]string{
		"top level":     "a: 1\n# c\nb: 2\n",
		"indented":      "g:\n  a: 1\n  # c\n  b: 2\n",
		"brace":         "g: {\n  a: 1\n  # c\n  b: 2\n}\n",
		"brace leading": "g: {\n  # c\n  a: 1\n}\n",
		"array":         "g: [\n  1\n  # c\n  2\n]\n",
		"multi-line":    "a: 1\n# one\n# two\nb: 2\n",
	}
	for name, src := range srcs {
		got, err := roundTripComments(t, src)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.Contains(got, "# c") && !strings.Contains(got, "# one") {
			t.Errorf("%s: comment dropped in round trip of %q -> %q", name, src, got)
		}
	}
}

// Whatever layout the encoder chooses for a value's preceding comment, it must not keep
// moving it: formatting an already-formatted document has to be a no-op.
func TestCommentRoundTripIsStable(t *testing.T) {
	srcs := []string{
		"a: 1\n# c\nb: 2\n",
		"g:\n  a: 1\n  # c\n  b: 2\n",
		"g: {\n  # c\n  a: 1\n}\n",
		"# lead\na: 1\n",
		"g: [\n  1\n  # c\n  2\n]\n",
	}
	for _, src := range srcs {
		first, err := roundTripComments(t, src)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		second, err := roundTripComments(t, first)
		if err != nil {
			t.Errorf("%q (second pass): %v", first, err)
			continue
		}
		if first != second {
			t.Errorf("round trip not stable for %q:\n  first  = %q\n  second = %q", src, first, second)
		}
	}
}

// A comment at the end of a container, with a value following outside it, is still
// dropped. The spec says it should cascade — "attributed to the preceding comments of the
// next value, which may be dedented or higher in the object notation" — but parseObj has
// no way to hand its trailing comments back to its caller. Pinned here so the day it
// starts working is a deliberate change and not a surprise.
func TestTrailingContainerCommentIsDroppedForNow(t *testing.T) {
	got, err := roundTripComments(t, "g: {\n  a: 1\n  # c\n}\nh: 3\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "# c") {
		t.Fatalf("trailing container comment is now preserved (%q) — the cascade rule may be "+
			"implemented; update this test and close the tracking issue", got)
	}
}
