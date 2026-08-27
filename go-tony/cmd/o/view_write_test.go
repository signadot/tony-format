package main

import (
	"os"
	"path/filepath"
	"testing"
)

// `o v -w` writes a document's normal form back over it, which is what makes
// normalising a tree one command. A reader accepts more than a writer produces --
// trailing whitespace, blank lines, quotes a value does not need -- and reading
// then writing is the whole of the normalisation (docs/tony.md, "Normalization").
func TestViewWriteNormalisesInPlace(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"trailing whitespace", "a: 1   \nb: 2\n", "a: 1\nb: 2\n"},
		{"blank lines", "a: 1\n\n\nb: 2\n", "a: 1\nb: 2\n"},
		{"quotes a value does not need", "a: \"x\"\nb: 'y'\n", "a: x\nb: y\n"},
		{"a line holding only whitespace", "a: 1\n   \nb: 2\n", "a: 1\nb: 2\n"},
		{"already normal", "a: 1\nb: 2\n", "a: 1\nb: 2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "doc.tony")
			if err := os.WriteFile(path, []byte(tc.in), 0o644); err != nil {
				t.Fatal(err)
			}
			if code, out := runOIn(t, "", "v", "-w", path); code != 0 {
				t.Fatalf("exit %d: %s", code, out)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// A file already in normal form is left alone rather than rewritten with identical
// bytes: formatting a tree should not touch the modification time of every file in
// it, which is what a build, a watch and a `git status` all read.
func TestViewWriteLeavesAnUnchangedFileAlone(t *testing.T) {
	dir := t.TempDir()
	same := filepath.Join(dir, "same.tony")
	changed := filepath.Join(dir, "changed.tony")
	if err := os.WriteFile(same, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(changed, []byte("a:   1   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(same)
	if err != nil {
		t.Fatal(err)
	}

	if code, out := runOIn(t, "", "v", "-w", same, changed); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}

	after, err := os.Stat(same)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("a file already in normal form was rewritten")
	}
	// and the one that needed it was written
	got, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a: 1\n" {
		t.Errorf("got %q, want %q", got, "a: 1\n")
	}
}

// The replacement takes the file's own mode, not the temporary file's.
func TestViewWriteKeepsTheFileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.tony")
	if err := os.WriteFile(path, []byte("a:   1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if code, out := runOIn(t, "", "v", "-w", path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mode is %v, want %v", got, os.FileMode(0o640))
	}
}

// Nothing is left beside the file: the replacement is written there and renamed
// over it, so an interrupted run leaves the original whole rather than a truncated
// file.
func TestViewWriteLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.tony")
	if err := os.WriteFile(path, []byte("a:   1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runOIn(t, "", "v", "-w", path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "doc.tony" {
		names := make([]string, len(ents))
		for i, e := range ents {
			names[i] = e.Name()
		}
		t.Errorf("the directory holds %v, want just doc.tony", names)
	}
}

// A document which does not parse leaves its file untouched: -w reads, normalises
// and only then writes, so a refusal costs nothing.
func TestViewWriteLeavesAnUnparseableFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.tony")
	const bad = "a: 1\np:\n" // a dangling ':' is refused
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _ := runOIn(t, "", "v", "-w", path)
	if code == 0 {
		t.Errorf("exit 0 on a document that does not parse")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != bad {
		t.Errorf("the file was written: got %q, want it untouched", got)
	}
}

// The three ways -w cannot be honoured, each a misuse rather than an empty answer.
func TestViewWriteRefusals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.tony")
	if err := os.WriteFile(path, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no file to write", []string{"v", "-w"}, "no file was named"},
		{"standard input", []string{"v", "-w", path, "-"}, "standard input"},
		{"colour into a file", []string{"v", "-w", "-color", path}, "-color"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _ := runOIn(t, "a: 1\n", tc.args...)
			if code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
		})
	}
	// and none of them wrote anything
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a: 1\n" {
		t.Errorf("a refused run wrote the file: %q", got)
	}
}
