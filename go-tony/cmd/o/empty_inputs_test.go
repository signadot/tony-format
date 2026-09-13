package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A patch that holds no document, a diff operand that holds none, and a loop
// interval of nothing are each a mistake to say so about, with exit 2. Each was a
// nil handed on, and a crash (p478tacqh12krg32msn0 item 20).
func TestEmptyInputsAreRefusedNotCrashedOn(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.tony")
	empty := filepath.Join(dir, "empty.tony")
	comments := filepath.Join(dir, "comments.tony")
	for p, c := range map[string]string{doc: "a: 1\n", empty: "", comments: "# only a comment\n"} {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"an empty patch", "", []string{"patch", "", doc}, "holds no document"},
		{"a patch of comments", "", []string{"patch", "# nothing\n", doc}, "holds no document"},
		{"an empty file as a diff side", "", []string{"diff", empty, doc}, "holds no document"},
		{"a file of comments as a diff side", "", []string{"diff", doc, comments}, "holds no document"},
		{"standard input holding nothing as a diff side", "", []string{"diff", doc}, "standard input holds no document"},
		{"a loop interval of nothing", "", []string{"diff", "-loop", "true", "-loopEvery", "0s"}, "positive"},
		{"a negative loop interval", "", []string{"diff", "-loop", "true", "-loopEvery", "-1s"}, "positive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runOBoth(t, tc.args...)
			if code != 2 {
				t.Errorf("exit %d, want 2\nstdout %q\nstderr %q", code, out, errOut)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Errorf("stderr %q, want it to say %q", errOut, tc.want)
			}
		})
	}
}
