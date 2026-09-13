package main

import (
	"testing"
)

// -each matches a list's elements rather than the list, writes the matching elements as
// one list gathered across every document, and treats a document that is not a list as
// matching nothing (zs0kk3azh12ksrksm9n0, xg534ta8h12ksegqmxn0). The stream is the issue's.
func TestMatchEachAsksAboutTheElements(t *testing.T) {
	const stream = "[]\n---\na: b # does not match\n---\n- a: b\n  c: d\n- a: b\n  c: 1\n- a: 2\n---\n- a: b\n  c: 2\n"
	code, out := runOIn(t, stream, "m", "-each", "a: b")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	const want = "- a: b\n  c: d\n- a: b\n  c: 1\n- a: b\n  c: 2\n"
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
	if _, out := runOIn(t, stream, "m", "-each", "-trim", "a: b"); out != "- a: b\n- a: b\n- a: b\n" {
		t.Errorf("-each -trim:\n%s", out)
	}
}

// Two -each stages compose as the conjunction of their patterns: the first writes a list,
// which is what the second reads (xg534ta8h12ksegqmxn0).
func TestMatchEachComposes(t *testing.T) {
	const stream = "- a: 1\n  b: 1\n- a: 1\n  b: 2\n- a: 2\n  b: 1\n---\n- a: 1\n  b: 1\n  c: 3\n"
	code, first := runOIn(t, stream, "m", "-each", "a: 1")
	if code != 0 {
		t.Fatalf("first stage: exit %d", code)
	}
	code, piped := runOIn(t, first, "m", "-each", "b: 1")
	if code != 0 {
		t.Fatalf("second stage: exit %d", code)
	}
	code, both := runOIn(t, stream, "m", "-each", "!and [{a: 1}, {b: 1}]")
	if code != 0 {
		t.Fatalf("conjunction: exit %d", code)
	}
	if piped != both {
		t.Errorf("piped:\n%s\nconjunction:\n%s", piped, both)
	}
	if want := "- a: 1\n  b: 1\n- a: 1\n  b: 1\n  c: 3\n"; both != want {
		t.Errorf("conjunction:\n%s\nwant\n%s", both, want)
	}
	// A first stage that keeps nothing writes nothing, and the second finds nothing.
	code, none := runOIn(t, stream, "m", "-each", "a: 9")
	if code != 1 || none != "" {
		t.Fatalf("empty first stage: exit %d, out %q", code, none)
	}
	if code, out := runOIn(t, none, "m", "-each", "b: 1"); code != 1 || out != "" {
		t.Errorf("second stage over nothing: exit %d, out %q", code, out)
	}
}
