package main

import (
	"testing"
)

// -each matches a list's elements rather than the list, writes each matching element as
// a document, and treats a document that is not a list as matching nothing
// (zs0kk3azh12ksrksm9n0). The stream is the issue's.
func TestMatchEachAsksAboutTheElements(t *testing.T) {
	const stream = "[]\n---\na: b # does not match\n---\n- a: b\n  c: d\n- a: b\n  c: 1\n- a: 2\n---\n- a: b\n  c: 2\n"
	code, out := runOIn(t, stream, "m", "-each", "a: b")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	const want = "a: b\nc: d\n---\na: b\nc: 1\n---\na: b\nc: 2\n"
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
	// Without -each the same pattern is asked about each document whole, and the one
	// document that is an object is the one that matches.
	code, out = runOIn(t, stream, "m", "a: b")
	if code != 0 || out != "a: b\n" {
		t.Errorf("without -each: exit %d, out\n%s", code, out)
	}
	// Nothing matched is grep's 1, for -each as for the rest.
	if code, out := runOIn(t, stream, "m", "-each", "a: zzz"); code != 1 || out != "" {
		t.Errorf("no element matched: exit %d, out %q", code, out)
	}
	// -trim trims each element.
	if _, out := runOIn(t, stream, "m", "-each", "-trim", "a: b"); out != "a: b\n---\na: b\n---\na: b\n" {
		t.Errorf("-each -trim:\n%s", out)
	}
}
