package main

import (
	"strings"
	"testing"
)

// `o help` is where a reader -- or an agent driving the tool -- chooses a command, so
// every command has to answer two questions there: what it is for, and how to call it.
// A command added without either still works and is still listed, and the listing is
// where that goes unnoticed, so it is checked rather than trusted.
//
// The summary is the description's first line. It must not merely repeat the name:
// "patch object documents" tells a reader nothing they did not have from the word
// "patch", which is what most of these said before.
func TestHelpListsWhatEachCommandIsFor(t *testing.T) {
	root := MainCommand()
	code, out, _ := runOBoth(t, "help")
	if code != 0 {
		t.Fatalf("o help exited %d", code)
	}
	for _, c := range root.Children {
		t.Run(c.Name, func(t *testing.T) {
			if strings.TrimSpace(c.Description) == "" {
				t.Fatal("no description, so the listing can say nothing about it")
			}
			if c.Synopsis == "" {
				t.Fatal("no synopsis, so the listing cannot say how to call it")
			}
			sum := summarize(c)
			if sum == "" {
				t.Fatal("the description's first line is empty")
			}
			if !strings.Contains(out, sum) {
				t.Errorf("the listing does not carry its summary %q", sum)
			}
			if !strings.Contains(out, c.Synopsis) {
				t.Errorf("the listing does not carry its synopsis %q", c.Synopsis)
			}
			// A summary of just the name, or the name twice, is the shape this
			// listing exists to avoid.
			if strings.EqualFold(sum, c.Name) {
				t.Errorf("the summary %q only repeats the command name", sum)
			}
		})
	}
}

// The conventions footer states things that are true of the commands which read
// documents, and it is worth only as much as it is accurate. The exit codes are the part
// a caller acts on, so they are the part pinned here.
func TestTheStatedExitCodesAreTheRealOnes(t *testing.T) {
	dir := t.TempDir()
	doc := writeDoc(t, dir, "d.tony", "a: 1\n")
	other := writeDoc(t, dir, "o.tony", "a: 2\n")

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"answered", []string{"get", ".a", doc}, 0},
		{"nothing found", []string{"get", ".zz", doc}, 1},
		{"a fault", []string{"get", ".a", "/nope.tony"}, 2},
		{"diff, the same", []string{"diff", doc, doc}, 0},
		{"diff, they differ", []string{"diff", doc, other}, 1},
		{"match, none", []string{"match", "{zz: 9}", doc}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, _, _ := runOBoth(t, tc.args...); code != tc.want {
				t.Errorf("exited %d, want %d -- `o help` says otherwise", code, tc.want)
			}
		})
	}
}
