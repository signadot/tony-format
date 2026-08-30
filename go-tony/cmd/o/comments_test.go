package main

import (
	"strings"
	"testing"
)

// The library could diff, patch and answer with comments; o could not ask for any
// of it. -c existed on view, dump and load only, so diff saw a comment-only
// change as no change, and patch rewrote a document without the comments it came
// in with -- which is how a comment-blind tool erases them
// (3cdjz00jh12krns4g1n0).

const commentedDoc = `# about the document
name: svc # after the name
spec:
  # about replicas
  replicas: 3
`

const commentedDoc2 = `# about the document, revised
name: svc # after the name
spec:
  # about replicas
  replicas: 3
`

// TestDiffComments: without -c a comment-only change is no change, and diff says
// so with an empty answer and exit 0. With -c it is a change, and the delta is
// the !comment operator rather than a replacement of the value described.
func TestDiffComments(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.tony", commentedDoc)
	b := writeDoc(t, dir, "b.tony", commentedDoc2)

	code, out := runO(t, "diff", a, b)
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Errorf("without -c a comment-only change diffed to %q (exit %d)", out, code)
	}

	code, out = runO(t, "diff", "-c", a, b)
	if code != 1 {
		t.Errorf("with -c the comment change did not report as a difference (exit %d, out %q)", code, out)
	}
	if !strings.Contains(out, "!comment") {
		t.Errorf("the delta does not use the comment operator: %q", out)
	}
	if !strings.Contains(out, "revised") {
		t.Errorf("the delta does not carry the new comment: %q", out)
	}
}

// TestPatchComments: the document being patched keeps what was said about it, and
// the patch's own comments arrive with it.
func TestPatchComments(t *testing.T) {
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.tony", commentedDoc)

	code, out := runO(t, "patch", "{other: 1}", doc)
	if code != 0 {
		t.Fatalf("patch failed: %d %q", code, out)
	}
	if strings.Contains(out, "#") {
		t.Errorf("without -c the result carries a comment: %q", out)
	}

	code, out = runO(t, "patch", "-c", "{other: 1}", doc)
	if code != 0 {
		t.Fatalf("patch -c failed: %d %q", code, out)
	}
	for _, want := range []string{"# about the document", "# after the name", "# about replicas", "other: 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("patch -c lost %q from\n%s", want, out)
		}
	}
}

// TestDiffPatchCommentRoundTrip is the property the two flags are for together:
// patching a with the diff of a and b gives b, comments and all.
func TestDiffPatchCommentRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.tony", commentedDoc)
	b := writeDoc(t, dir, "b.tony", commentedDoc2)

	code, delta := runO(t, "diff", "-c", a, b)
	if code != 1 {
		t.Fatalf("expected a difference, got exit %d", code)
	}
	deltaFile := writeDoc(t, dir, "delta.tony", delta)

	code, out := runO(t, "patch", "-c", "-f", deltaFile, a)
	if code != 0 {
		t.Fatalf("patch -c failed: %d %q", code, out)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(commentedDoc2) {
		t.Errorf("the round trip gave\n%s\nand b is\n%s", out, commentedDoc2)
	}
}

// TestGetListComments: a path ANSWERS with the value it names, which drops what
// was said above it. With -c the caller is asking to be shown the document, so
// the answer keeps it.
func TestGetListComments(t *testing.T) {
	dir := t.TempDir()
	doc := writeDoc(t, dir, "doc.tony", commentedDoc)

	code, out := runO(t, "get", "-c", ".spec", doc)
	if code != 0 {
		t.Fatalf("get -c failed: %d %q", code, out)
	}
	if !strings.Contains(out, "# about replicas") {
		t.Errorf("get -c lost the head comment on the value it answered with: %q", out)
	}

	code, out = runO(t, "get", ".spec", doc)
	if code != 0 || strings.Contains(out, "#") {
		t.Errorf("get without -c answered with a comment: %q", out)
	}

	list := writeDoc(t, dir, "list.tony", "items:\n- a # first\n- b\n")
	code, out = runO(t, "list", "-c", ".items[*]", list)
	if code != 0 {
		t.Fatalf("list -c failed: %d %q", code, out)
	}
	if !strings.Contains(out, "# first") {
		t.Errorf("list -c lost an element's comment: %q", out)
	}
}

// TestMatchIsBlindToComments: matching asks about the value and sees through what
// was said about it, with -c or without. The flag decides what the ANSWER
// carries, not what matches.
func TestMatchIsBlindToComments(t *testing.T) {
	dir := t.TempDir()
	a := writeDoc(t, dir, "a.tony", commentedDoc)
	b := writeDoc(t, dir, "b.tony", commentedDoc2)

	for _, args := range [][]string{
		{"match", "-f", b, a},
		{"match", "-c", "-f", b, a},
	} {
		code, out := runO(t, args...)
		if code != 0 {
			t.Errorf("%v: documents differing only in a comment did not match (exit %d, %q)", args, code, out)
		}
	}

	code, out := runO(t, "match", "-c", "-f", b, a)
	if code == 0 && !strings.Contains(out, "# about the document") {
		t.Errorf("match -c answered without comments: %q", out)
	}
}
