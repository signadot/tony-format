package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `-` names standard input, and standard input is the CONTEXT's: cc.In is what a
// caller can redirect, which is what makes the command tree drivable in process --
// by a test, or by anything embedding it.
//
// Five readers took os.Stdin instead. From a shell that works, because os.Stdin IS
// the pipe; in process it reads the real process input and answers nothing. So every
// `-` path was unreachable in a test, and wrong for an embedder.
//
// The rule was already written down at eval.go's no-argument branch -- "cc.In, not
// os.Stdin: the context is what a caller can redirect, and eval was the one reader of
// standard input that could not be" -- and eval itself broke it forty lines below.
func TestDashReadsTheContextsInput(t *testing.T) {
	const doc = "a: 1\n"
	path := writeTemp(t, doc)
	// a second file for the case whose stdin is a PATTERN rather than a document
	docPath := writeTemp(t, doc)

	// Compared against the same bytes in a FILE rather than against an expected
	// rendering: what is being tested is where the input came from, and each
	// command answers its own question about it.
	for _, tc := range []struct {
		name string
		args func(src string) []string
	}{
		{"view", func(src string) []string { return []string{"v", src} }},
		{"dump", func(src string) []string { return []string{"dump", src} }},
		{"load", func(src string) []string { return []string{"load", src} }},
		{"eval", func(src string) []string { return []string{"eval", src} }},
		// match reads TWO things, and the one getish handles is the PATTERN, from
		// -f. The document argument was never the broken reader, so passing the
		// pattern plainly exercises nothing -- as `-f src .a` did not either, where
		// .a was read as a filename and both paths failed identically.
		{"match pattern", func(src string) []string { return []string{"match", "-f", src, docPath} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := doc
			src := path
			if strings.Contains(tc.name, "pattern") {
				in = "{a: 1}\n"
				src = writeTemp(t, in)
			}
			fileCode, fromFile := runOIn(t, "", tc.args(src)...)
			dashCode, fromDash := runOIn(t, in, tc.args("-")...)
			if dashCode != fileCode {
				t.Fatalf("`-` exited %d where the file exited %d\n from file: %q\n from -:    %q",
					dashCode, fileCode, fromFile, fromDash)
			}
			if fromDash != fromFile {
				t.Errorf("`-` read something other than the context's input\n from file: %q\n from -:    %q",
					fromFile, fromDash)
			}
			if fileCode == 0 && fromFile == "" {
				t.Skip("this command answers nothing for the fixture; the comparison says little")
			}
		})
	}
}

// and naming a file still reads the file, not the input
func TestDashDoesNotShadowAFile(t *testing.T) {
	path := writeTemp(t, "b: 2\n")
	code, out := runOIn(t, "a: 1\n", "v", path)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if !strings.Contains(out, "b: 2") {
		t.Errorf("got %q, want the file's contents", out)
	}
	if strings.Contains(out, "a: 1") {
		t.Errorf("read standard input for a named file: %q", out)
	}
}

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.tony")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
