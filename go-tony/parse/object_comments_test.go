package parse

import (
	"bytes"
	"strings"
	"testing"
	"time"

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

// A comment at the end of a container cascades to the next value, which the spec puts as
// "attributed to the preceding comments of the next value, which may be dedented or higher
// in the object notation". Indented objects and both array forms always did; a brace object
// dropped its last comment, because parseObj buckets head comments by the pair they precede
// and the last bucket heads no pair (23b00eyvh12ksgebcxn0).
func TestTrailingContainerCommentCascades(t *testing.T) {
	cases := map[string]struct{ src, want string }{
		"brace, value follows":   {"g: {\n  a: 1\n  # c\n}\nh: 3\n", "g: {\n  a: 1\n}\n# c\nh: 3\n"},
		"brace, end of document": {"g: {\n  a: 1\n  # c\n}\n", "g: {\n  a: 1\n}\n# c\n"},
		"brace, empty object":    {"g: {\n  # c\n}\nh: 3\n", "g: {}\n# c\nh: 3\n"},
		"brace, two lines":       {"g: {\n  a: 1\n  # c\n  # d\n}\nh: 3\n", "g: {\n  a: 1\n}\n# c\n# d\nh: 3\n"},
		"indented object":        {"g:\n  a: 1\n  # c\nh: 3\n", "g:\n  a: 1\n# c\nh: 3\n"},
		"bracket array":          {"g: [\n  1\n  # c\n]\nh: 3\n", "g: [\n  1\n]\n# c\nh: 3\n"},
		"block array":            {"g:\n- 1\n# c\nh: 3\n", "g:\n- 1\n# c\nh: 3\n"},
	}
	for name, c := range cases {
		got, err := roundTripComments(t, c.src)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: round trip of %q\n  got  %q\n  want %q", name, c.src, got, c.want)
			continue
		}
		// Where the comment lands has to be a fixed point, or every read-write pass
		// walks it one value further along.
		again, err := roundTripComments(t, got)
		if err != nil {
			t.Errorf("%s (second pass): %v", name, err)
			continue
		}
		if again != got {
			t.Errorf("%s: not stable\n  first  %q\n  second %q", name, got, again)
		}
	}
}

// "a: {} # c" used to hang. The line comment after a collection value was left on the
// input — only scalars consume their own in noComments — and parseObj's default arm
// ignored it without advancing, so the loop spun forever. The comment now lands on the
// collection, as it already did for an array element ("- {} # c"), and encodes after the
// closing token.
func TestLineCommentAfterCollectionValue(t *testing.T) {
	// The encoder spreads a non-empty bracket collection over lines regardless of how it
	// was written, so want is not always src; what matters is where the comment lands.
	for _, tc := range []struct{ src, want string }{
		{"a: {} # c\n", "a: {} # c\n"},
		{"a: [] # c\n", "a: [] # c\n"},
		{"a: {b: 1} # c\n", "a: {\n  b: 1\n} # c\n"},
		{"a: [1] # c\n", "a: [\n  1\n] # c\n"},
		{"a:\n  b: {} # c\n  d: 2\n", "a:\n  b: {} # c\n  d: 2\n"},
		{"- {} # c\n- 1\n", "- {} # c\n- 1\n"},
		{"a: !tag {} # c\n", "a: !tag {} # c\n"},
	} {
		done := make(chan struct{})
		var got string
		var err error
		go func() {
			defer close(done)
			got, err = roundTripComments(t, tc.src)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%q: parse did not terminate", tc.src)
		}
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		if got != tc.want {
			t.Errorf("round trip %q = %q, want %q", tc.src, got, tc.want)
		}
	}
}
