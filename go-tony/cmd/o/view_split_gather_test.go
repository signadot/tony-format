package main

import (
	"os"
	"path/filepath"
	"testing"
)

// -split writes each element of every list document as a document of its own, and
// nothing for a document that is not a list; -gather writes every document of every
// input as one list, and [] for none (xg534ta8h12ksegqmxn0).
func TestViewSplitAndGather(t *testing.T) {
	const lists = "- a: 1\n- a: 2\n---\nx: scalar\n---\n- a: 3\n"
	code, out := runOIn(t, lists, "v", "-split")
	if code != 0 {
		t.Fatalf("-split: exit %d", code)
	}
	if want := "a: 1\n---\na: 2\n---\na: 3\n"; out != want {
		t.Errorf("-split:\n%s\nwant\n%s", out, want)
	}

	code, out = runOIn(t, "a: 1\n---\na: 2\n---\n- x\n", "v", "-gather")
	if code != 0 {
		t.Fatalf("-gather: exit %d", code)
	}
	if want := "- a: 1\n- a: 2\n- - x\n"; out != want {
		t.Errorf("-gather:\n%s\nwant\n%s", out, want)
	}

	// Gathering nothing is a list of nothing; splitting nothing is nothing.
	if code, out := runOIn(t, "", "v", "-gather"); code != 0 || out != "[]\n" {
		t.Errorf("-gather of no documents: exit %d, out %q", code, out)
	}
	if code, out := runOIn(t, "x: scalar\n", "v", "-split"); code != 0 || out != "" {
		t.Errorf("-split of no lists: exit %d, out %q", code, out)
	}

	// -gather reads every input as one stream: two files, one list.
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.tony"), filepath.Join(dir, "b.tony")
	os.WriteFile(a, []byte("n: 1\n"), 0o644)
	os.WriteFile(b, []byte("n: 2\n---\nn: 3\n"), 0o644)
	if code, out := runOIn(t, "", "v", "-gather", a, b); code != 0 || out != "- n: 1\n- n: 2\n- n: 3\n" {
		t.Errorf("-gather of two files: exit %d, out\n%s", code, out)
	}

	// Each undoes the other.
	_, split := runOIn(t, "- a: 1\n- b: 2\n", "v", "-split")
	if _, back := runOIn(t, split, "v", "-gather"); back != "- a: 1\n- b: 2\n" {
		t.Errorf("-split then -gather:\n%s", back)
	}

	// Refused: both at once, and either with -w.
	if code, _ := runOIn(t, "", "v", "-split", "-gather"); code != 2 {
		t.Errorf("-split -gather: exit %d, want 2", code)
	}
	if code, _ := runOIn(t, "", "v", "-w", "-gather", a); code != 2 {
		t.Errorf("-w -gather: exit %d, want 2", code)
	}
}

// -each is split, match, gather, whenever an element matched.
func TestMatchEachIsSplitMatchGather(t *testing.T) {
	const stream = "- state: open\n  kind: bug\n- state: closed\n  kind: bug\n---\n- state: open\n  kind: feat\n- state: open\n  kind: bug\n"
	_, each := runOIn(t, stream, "m", "-each", "!and [{state: open}, {kind: bug}]")
	_, split := runOIn(t, stream, "v", "-split")
	_, open := runOIn(t, split, "m", "state: open")
	_, bugs := runOIn(t, open, "m", "kind: bug")
	_, gathered := runOIn(t, bugs, "v", "-gather")
	if each != gathered {
		t.Errorf("-each:\n%s\nsplit | m | m | gather:\n%s", each, gathered)
	}
	if want := "- state: open\n  kind: bug\n- state: open\n  kind: bug\n"; each != want {
		t.Errorf("-each:\n%s\nwant\n%s", each, want)
	}
}
