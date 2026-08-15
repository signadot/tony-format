package ir_test

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

const commented = "# lead\na:\n  # about the object\n  b: 1\nz: 2 # latch\n"

func commentedDoc(t *testing.T) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(commented), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return n
}

// TestNavigationSeesThroughComments: a path names a value, and a comment
// describes one. A head comment wraps the value it precedes, so a commented
// document had a wrapper between every container and its contents -- and the
// walks stopped there: GetKPath and GetPath answered "expected object, got
// Comment" and ListPath panicked outright.
//
// The index, reads, scope-owned paths and watches are all path-addressed, so
// one comment at the root made a whole document unaddressable.
func TestNavigationSeesThroughComments(t *testing.T) {
	n := commentedDoc(t)

	for _, tc := range []struct{ kpath, want string }{
		{"a", "b: 1"},
		{"a.b", "1"},
		{"z", "2"},
	} {
		got, err := n.GetKPath(tc.kpath)
		if err != nil {
			t.Errorf("GetKPath(%q): %v", tc.kpath, err)
			continue
		}
		if strings.TrimSpace(encode.MustString(got)) != tc.want {
			t.Errorf("GetKPath(%q) = %q, want %q", tc.kpath, encode.MustString(got), tc.want)
		}
	}

	for _, tc := range []struct{ path, want string }{
		{"$.a", "b: 1"},
		{"$.a.b", "1"},
		{"$.z", "2"},
	} {
		got, err := n.GetPath(tc.path)
		if err != nil {
			t.Errorf("GetPath(%q): %v", tc.path, err)
			continue
		}
		if strings.TrimSpace(encode.MustString(got)) != tc.want {
			t.Errorf("GetPath(%q) = %q, want %q", tc.path, encode.MustString(got), tc.want)
		}
		list, err := n.ListPath(nil, tc.path)
		if err != nil {
			t.Errorf("ListPath(%q): %v", tc.path, err)
			continue
		}
		if len(list) != 1 {
			t.Errorf("ListPath(%q) returned %d nodes, want 1", tc.path, len(list))
		}
	}
}

// TestNavigationWithComments: the option says what to do with a comment at the
// END of the walk. By default the value is answered, since that is what a path
// names; with it, the node as it stands, for a caller that came looking for
// what was said about the value.
func TestNavigationWithComments(t *testing.T) {
	n := commentedDoc(t)

	plain, err := n.GetKPathWith("a")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Type != ir.ObjectType {
		t.Errorf("without the option the walk answered a %v, want the value", plain.Type)
	}

	kept, err := n.GetKPathWith("a", ir.WithComments(true))
	if err != nil {
		t.Fatal(err)
	}
	if kept.Type != ir.CommentType {
		t.Fatalf("with the option the walk answered a %v, want the comment it carries", kept.Type)
	}
	var b strings.Builder
	if err := encode.Encode(kept, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "# about the object") {
		t.Errorf("the comment did not come with it:\n%s", b.String())
	}
}

// TestNavigationUnchangedWithoutComments: a document with no comments navigates
// exactly as it did.
func TestNavigationUnchangedWithoutComments(t *testing.T) {
	n, err := parse.Parse([]byte("a:\n  b: 1\nz: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kpath, want string }{{"a.b", "1"}, {"z", "2"}} {
		got, err := n.GetKPath(tc.kpath)
		if err != nil {
			t.Fatalf("GetKPath(%q): %v", tc.kpath, err)
		}
		if strings.TrimSpace(encode.MustString(got)) != tc.want {
			t.Errorf("GetKPath(%q) = %q, want %q", tc.kpath, encode.MustString(got), tc.want)
		}
	}
}

// TestDeepEqualIsCommentBlind: equality is what decides whether a subtree
// changed -- logd's watch path asks it twice and its head agreement once -- and
// it was the one question in this package that counted comments. A document
// differing only in a comment therefore read as changed, and two
// materializations keeping different halves of the comments read as divergent.
func TestDeepEqualIsCommentBlind(t *testing.T) {
	p := func(src string) *ir.Node {
		t.Helper()
		n, err := parse.Parse([]byte(src), parse.ParseComments(true))
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		return n
	}

	for _, tc := range []struct{ name, a, b string }{
		{"a line comment differs", "name: svc # one\n", "name: svc # two\n"},
		{"a head comment differs", "# one\nname: svc\n", "# two\nname: svc\n"},
		{"one has a comment, the other none", "# one\nname: svc\n", "name: svc\n"},
		{"a comment deep inside", "a:\n  # one\n  b: 1\n", "a:\n  b: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := p(tc.a), p(tc.b)
			if !a.DeepEqual(b) {
				t.Errorf("the same data read as different: %q against %q", tc.a, tc.b)
			}
			if a.DeepEqualWithComments(b) {
				t.Errorf("DeepEqualWithComments ignored the comments: %q against %q", tc.a, tc.b)
			}
		})
	}

	// and data differences are still differences
	if p("a: 1\n").DeepEqual(p("a: 2\n")) {
		t.Error("different data read as equal")
	}
}

// TestStripComments: "without comments" is both kinds. A head comment is a
// wrapper and a line comment is a field on the node, so dropping one is not
// dropping the other -- which is how a patch with comments off used to keep the
// line comments.
func TestStripComments(t *testing.T) {
	n, err := parse.Parse([]byte("# lead\na:\n  # about\n  b: 1 # latch\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	stripped := ir.StripComments(n)
	var b strings.Builder
	if err := encode.Encode(stripped, &b, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "#") {
		t.Errorf("a comment survived StripComments:\n%s", b.String())
	}
	if v, err := stripped.GetKPath("a.b"); err != nil || v == nil {
		t.Errorf("stripping broke the structure: %v %v", v, err)
	}
}
