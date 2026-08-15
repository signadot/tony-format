package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
)

// TestPatchWholeDocumentDelete: a patch whose result is a deletion used to
// segfault. tony.Patch reports a delete by returning a nil node -- a convention
// the storage paths guard and this one did not -- and encode dereferenced it
// (issue a7bwkxwah12kr0n0fxn0).
//
// Deleting everything is a result, not a fault: nothing is written and the exit
// code is 0.
func TestPatchWholeDocumentDelete(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.tony")
	if err := os.WriteFile(doc, []byte("- {name: a, state: open}\n- {name: b, state: closed}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, patch string }{
		{"an outright delete", "!delete null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outBuf, errBuf := &strings.Builder{}, &strings.Builder{}
			cc := &cli.Context{Out: nopWC{outBuf}, Err: nopWC{errBuf}, In: io.NopCloser(strings.NewReader(""))}
			cmd := MainCommand()
			err := cmd.Run(cc, []string{"patch", "-s", tc.patch, doc})
			if code := cmd.Exit(cc, err); code != 0 {
				t.Fatalf("exit %d, want 0 (stderr: %s)", code, errBuf.String())
			}
			if outBuf.String() != "" {
				t.Fatalf("wrote %q, want nothing: the document is gone", outBuf.String())
			}
		})
	}
}
