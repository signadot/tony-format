package main

import (
	"os"
	"path/filepath"
	"testing"
)

// `-o f` opens f for writing, and empties it, while the options are being parsed,
// before the command reads anything. So `o -o in.tony v in.tony` -- naming the
// input as the output -- read an empty file and exited 0 with in.tony empty. It is
// a misuse, and it is now refused as one, with the file left as it was.
func TestOutputNamingAnInputIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "in.tony")
	const held = "a:   1   \nb: 2\n"
	for _, args := range [][]string{
		{"-o", path, "v", path},
		{"v", "-o", path, path},
		{"-o", path, "get", ".a", filepath.Join(dir, "other.tony"), path},
	} {
		if err := os.WriteFile(path, []byte(held), 0o600); err != nil {
			t.Fatal(err)
		}
		code, _ := runOIn(t, "", args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2 (usage)", args, code)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != held {
			t.Errorf("%v: the file holds %q, want it untouched", args, got)
		}
	}

	// A different file as the output, and the same file spelled once, are not misuses.
	out := filepath.Join(dir, "out.tony")
	if code, o := runOIn(t, "", "-o", out, "v", path); code != 0 {
		t.Fatalf("-o out v in: exit %d: %s", code, o)
	}
	if got, _ := os.ReadFile(out); string(got) != "a: 1\nb: 2\n" {
		t.Errorf("out.tony holds %q", got)
	}
	if code, o := runOIn(t, "a: 1\n", "-o", path, "v"); code != 0 {
		t.Fatalf("-o in v (stdin): exit %d: %s", code, o)
	}
	if got, _ := os.ReadFile(path); string(got) != "a: 1\n" {
		t.Errorf("in.tony holds %q, want the view of stdin", got)
	}
}
