package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestEvalAllEvaluatesAnUntaggedDocument: a file written FOR eval reads as one that
// needs no !eval at the top of it, and without the tag `o eval` answers with the input,
// expanding nothing and saying nothing about why (37b227kfh12ks8atphn0). -a is the
// answer to that, and the default is untouched: an untagged document someone else wrote
// still comes back as it was, its $[x] the text $[x].
func TestEvalAllEvaluatesAnUntaggedDocument(t *testing.T) {
	const doc = "x: hi\ngreet: '$[x]'\n"
	path := writeDoc(t, t.TempDir(), "whole.tony", doc)

	code, out := runO(t, "eval", "-e", "x=hi", path)
	if code != 0 {
		t.Fatalf("exited %d: %s", code, out)
	}
	if !strings.Contains(out, "$[x]") {
		t.Errorf("without -a the expression was expanded: %s", out)
	}

	for _, flag := range []string{"-a", "-all"} {
		code, out := runO(t, "eval", flag, "-e", "x=hi", path)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", flag, code, out)
		}
		if strings.Contains(out, "$[x]") || !strings.Contains(out, "greet: hi") {
			t.Errorf("%s did not evaluate the whole document: %s", flag, out)
		}
	}
}

// TestEvalAllDoesNotEvaluateTwice: a document that already says !eval is left as it is,
// so what one expansion produced is not expanded again -- here the value of y is the
// TEXT $[x], and it stays that text.
func TestEvalAllDoesNotEvaluateTwice(t *testing.T) {
	const doc = "!eval\nout: '$[y]'\n"
	path := writeDoc(t, t.TempDir(), "tagged.tony", doc)

	code, out := runO(t, "eval", "-a", "-e", `y='$[x]'`, "-e", "x=hi", path)
	if code != 0 {
		t.Fatalf("exited %d: %s", code, out)
	}
	if !strings.Contains(out, "$[x]") {
		t.Errorf("-a expanded what an expansion produced: %s", out)
	}
}

// TestEvalAllComposesWithTheRootsOwnTag: the tag is composed onto whatever the root
// carries and applied last, so !file still loads and -a expands what it loaded.
func TestEvalAllComposesWithTheRootsOwnTag(t *testing.T) {
	dir := t.TempDir()
	writeDoc(t, dir, "held.tony", "greet: '$[x]'\n")
	path := writeDoc(t, dir, "loader.tony", "!tovalue.file "+filepath.Join(dir, "held.tony")+"\n")

	code, out := runO(t, "eval", "-a", "-e", "x=hi", path)
	if code != 0 {
		t.Fatalf("exited %d: %s", code, out)
	}
	if !strings.Contains(out, "greet: hi") {
		t.Errorf("-a did not expand what !file loaded: %s", out)
	}
}
