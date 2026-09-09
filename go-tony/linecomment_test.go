package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

func rendered(t *testing.T, n *ir.Node) string {
	t.Helper()
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// A patch keeping comments keeps the line comment on an object-valued key, not only the
// ones on scalars: a merged container is a fresh node, and it takes the patch's note
// (040x4f26h12kr5acm5n0).
func TestPatchKeepsTheLineCommentOnAnObjectKey(t *testing.T) {
	const doc = "# about this pr\nstage: open # still open\nlabels: # what it is tagged with\n  urgent: true # by whom\n"
	patch, err := parse.Parse([]byte(doc), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	for _, esc := range []string{"", "!raw", "!insert.raw", "!insert"} {
		p := patch.Clone()
		if esc != "" {
			p = p.WithTag(esc)
		}
		out, err := Patch(ir.Null(), p, mergeop.Comments(true))
		if err != nil {
			t.Fatalf("%q: %v", esc, err)
		}
		got := rendered(t, out)
		for _, want := range []string{"# about this pr", "# still open", "# what it is tagged with", "# by whom"} {
			if !strings.Contains(got, want) {
				t.Errorf("under %q the patch lost %q:\n%s", esc, want, got)
			}
		}
	}
}

// The patch's note is the more recent statement; the document's stands when the patch
// made none.
func TestMergedContainerTakesTheNewerLineComment(t *testing.T) {
	doc := mustParseComments(t, "labels: # old note\n  a: 1\n")
	quiet := mustParseComments(t, "labels:\n  b: 2\n")
	noted := mustParseComments(t, "labels: # new note\n  b: 2\n")
	if got := rendered(t, mustPatch(t, doc, quiet)); !strings.Contains(got, "# old note") {
		t.Errorf("a patch with no note dropped the document's:\n%s", got)
	}
	got := rendered(t, mustPatch(t, doc, noted))
	if !strings.Contains(got, "# new note") || strings.Contains(got, "# old note") {
		t.Errorf("a patch with a note did not replace the document's:\n%s", got)
	}
}

func mustParseComments(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func mustPatch(t *testing.T, doc, patch *ir.Node) *ir.Node {
	t.Helper()
	out, err := Patch(doc, patch, mergeop.Comments(true))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
