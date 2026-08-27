package main

import (
	"os"
	"path/filepath"
	"strings"
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

// -w keeps comments whether or not -c was given. Dropping them is a display choice
// for a command which PRINTS; for one which overwrites the source it is data loss,
// and it went unnoticed because every other test here uses a file without one.
func TestViewWriteKeepsComments(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{
			name: "a head comment and a line comment",
			in:   "# a header\na: 1 # why\nb: 2\n",
			want: "# a header\na: 1 # why\nb: 2\n",
		},
		{
			name: "a comment above a nested key",
			in:   "a:\n  # about b\n  b: 1\n",
			want: "a:\n  # about b\n  b: 1\n",
		},
		{
			// this came back zero bytes
			name: "a file holding nothing but a comment",
			in:   "# just a note\n",
			want: "# just a note\n",
		},
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
			if len(got) == 0 {
				t.Fatalf("the file was emptied")
			}
			if !strings.HasPrefix(string(got), tc.want) {
				t.Errorf("got %q, want it to start with %q", got, tc.want)
			}
		})
	}
}

// Formatting is idempotent: what -w writes is what -w reads without changing it.
// A formatter which does not settle rewrites a tree on every run.
func TestViewWriteIsIdempotent(t *testing.T) {
	for _, in := range []string{
		"a: 1   \n\n\nb: \"x\"\n",
		"# a header\na: 1 # why\n",
		"# just a note\n",
		"a: 1\n---\nb: 2\n---\nc: 3\n",
		"a:\n  # about b\n  b: 1\n",
	} {
		path := filepath.Join(t.TempDir(), "doc.tony")
		if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, out := runOIn(t, "", "v", "-w", path); code != 0 {
			t.Fatalf("%q: exit %d: %s", in, code, out)
		}
		once, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if code, out := runOIn(t, "", "v", "-w", path); code != 0 {
			t.Fatalf("%q: second run exit %d: %s", in, code, out)
		}
		twice, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(once) != string(twice) {
			t.Errorf("%q: not settled\n once:  %q\n twice: %q", in, once, twice)
		}
	}
}

// A separator does not need a newline the document has already written. Writing
// "\n---\n" after a document which ends in one put a blank line before every
// separator -- a line the author did not write, and one the writer is documented
// to drop.
func TestViewWriteSeparatesWithoutABlankLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.tony")
	const in = "a: 1\n---\nb: 2\n---\nc: 3\n"
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runOIn(t, "", "v", "-w", path); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "\n\n---") {
		t.Errorf("a blank line before a separator: %q", got)
	}
	if string(got) != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

// Through a symlink, -w formats what the link NAMES. Renaming over the link would
// replace it with a regular file and leave the target unformatted, which detaches
// a symlinked config from the thing it points at.
func TestViewWriteFollowsASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.tony")
	link := filepath.Join(dir, "link.tony")
	if err := os.WriteFile(target, []byte("x:   1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.tony", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if code, out := runOIn(t, "", "v", "-w", link); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the symlink was replaced with a regular file")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "x: 1\n" {
		t.Errorf("the target was not formatted: %q", got)
	}
}
