package tony_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

func withC(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

func shown(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return ""
	}
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b.String()
}

// TestPatchComments: mergeop.Comments(true) is what the format means by
// "patching comments if so desired". It existed, was documented, and nothing
// read it -- so a head comment vanished through any patch, and since a write is
// a patch, through any store.
func TestPatchComments(t *testing.T) {
	doc := ir.Null()
	patch := withC(t, "# lead\nname: svc # latch\n")

	off, err := tony.Patch(doc, patch)
	if err != nil {
		t.Fatal(err)
	}
	if got := shown(t, off); strings.Contains(got, "# lead") {
		t.Errorf("without the option a head comment came through:\n%s", got)
	}

	on, err := tony.Patch(doc, patch, mergeop.Comments(true))
	if err != nil {
		t.Fatal(err)
	}
	got := shown(t, on)
	for _, want := range []string{"# lead", "name: svc", "# latch"} {
		if !strings.Contains(got, want) {
			t.Errorf("with the option, %q did not survive:\n%s", want, got)
		}
	}
}

// TestPatchCommentsPrefersThePatch: the patch's comment is the more recent
// statement about the value; the document's stands when the patch says nothing.
func TestPatchCommentsPrefersThePatch(t *testing.T) {
	doc := withC(t, "# old\nname: was\n")

	newer, err := tony.Patch(doc, withC(t, "# new\nname: now\n"), mergeop.Comments(true))
	if err != nil {
		t.Fatal(err)
	}
	if got := shown(t, newer); !strings.Contains(got, "# new") || strings.Contains(got, "# old") {
		t.Errorf("the patch's comment did not win:\n%s", got)
	}

	kept, err := tony.Patch(doc, withC(t, "name: now\n"), mergeop.Comments(true))
	if err != nil {
		t.Fatal(err)
	}
	if got := shown(t, kept); !strings.Contains(got, "# old") {
		t.Errorf("the document's comment was dropped by a patch that said nothing about it:\n%s", got)
	}
}

// TestPatchCommentsLeaveTheDataAlone: whatever happens to comments, the value
// is the value. A patch that keeps comments must patch exactly as one that
// drops them.
func TestPatchCommentsLeaveTheDataAlone(t *testing.T) {
	for _, tc := range []struct{ name, doc, patch string }{
		{"a field added", "a: 1\n", "# c\nb: 2\n"},
		{"a field replaced", "a: 1\n", "# c\na: 2 # l\n"},
		{"a merge key", "a: 1\n<<: {m: 1}\nz: 2\n", "# c\nz: 3\n"},
		{"nested", "a:\n  b: 1\n", "a:\n  # c\n  b: 2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, p := withC(t, tc.doc), withC(t, tc.patch)
			plain, err := tony.Patch(d, p)
			if err != nil {
				t.Fatal(err)
			}
			kept, err := tony.Patch(d, p, mergeop.Comments(true))
			if err != nil {
				t.Fatal(err)
			}
			// Compare the DATA: encode both without comments.
			var a, b strings.Builder
			if err := encode.Encode(plain, &a); err != nil {
				t.Fatal(err)
			}
			if err := encode.Encode(kept, &b); err != nil {
				t.Fatal(err)
			}
			if a.String() != b.String() {
				t.Fatalf("keeping comments changed the data:\nwithout:\n%s\nwith:\n%s", a.String(), b.String())
			}
		})
	}
}

// TestPatchWithoutCommentsDropsBoth: "comments off" is both kinds. A head
// comment is a wrapper, discarded by anything that descends through it; a line
// comment rides on the node and every clone carried it along. So off used to
// mean "the head ones", and a store that asked for no comments kept half.
func TestPatchWithoutCommentsDropsBoth(t *testing.T) {
	for _, src := range []string{
		"# head\nname: svc\n",
		"name: svc # latch\n",
		"# head\nname: svc # latch\n",
		"a:\n  # about\n  b: 1 # latch\n",
	} {
		got, err := tony.Patch(ir.Null(), withC(t, src))
		if err != nil {
			t.Fatalf("patch %q: %v", src, err)
		}
		if out := shown(t, got); strings.Contains(out, "#") {
			t.Errorf("a comment survived a patch that did not ask for comments:\n%s", out)
		}
	}
}
