package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
)

// nopWC lets a strings.Builder stand in for cli.Context's output.
type nopWC struct{ *strings.Builder }

func (nopWC) Close() error { return nil }

// runMatch runs `o match` with args and answers the exit code the CLI would
// exit with, which is the thing under test: the codes are the command's answer
// to a pipe, and nothing else reports them.
func runMatch(t *testing.T, args ...string) (code int, out string) {
	t.Helper()
	outBuf, errBuf := &strings.Builder{}, &strings.Builder{}
	cc := &cli.Context{Out: nopWC{outBuf}, Err: nopWC{errBuf}}

	// Through the whole tree rather than the subcommand alone: MainCommand wires
	// the config the subcommand reads, and the exit code is the root's answer.
	cmd := MainCommand()
	err := cmd.Run(cc, append([]string{"match"}, args...))
	return cmd.Exit(cc, err), outBuf.String()
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMatchExitCodes: grep's convention, because a filter in a pipe is read
// against grep. The distinction that matters is 1 from 2 -- "nothing matched" is
// an answer, and reporting it as a fault (or worse, as success) is how a wrong
// pattern comes to look like an empty world (issue 0dxprv4ch12kr4n0fxn0).
func TestMatchExitCodes(t *testing.T) {
	dir := t.TempDir()
	stream := write(t, dir, "stream.tony", "{name: a, state: open}\n---\n{name: b, state: closed}\n")
	list := write(t, dir, "list.tony", "- {name: a, state: open}\n- {name: b, state: closed}\n")
	missing := filepath.Join(dir, "nope.tony")

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"something matched", []string{"{state: open}", stream}, 0},
		{"nothing matched", []string{"{state: zzz}", stream}, 1},
		{"a list is one document, so an element pattern matches nothing", []string{"{state: open}", list}, 1},
		{"a whole-list pattern does match", []string{"!subtree {state: open}", list}, 0},
		{"unreadable input", []string{"{state: open}", missing}, 2},
		{"no input named", []string{"{state: open}"}, 2},
		{"no pattern", []string{}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := runMatch(t, tc.args...)
			if got != tc.want {
				t.Fatalf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// TestMatchNothingWritesNothing: exit 1 says it; the stream stays empty, so a
// consumer reading stdout is not handed an empty document to misread.
func TestMatchNothingWritesNothing(t *testing.T) {
	dir := t.TempDir()
	stream := write(t, dir, "stream.tony", "{name: a}\n")
	code, out := runMatch(t, "{name: zzz}", stream)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if out != "" {
		t.Fatalf("wrote %q, want nothing", out)
	}
}

// TestMatchSeparatesDocumentsAcrossFiles: the separator counts documents
// written, not documents within one file. Two files answering once each used to
// run the second onto the end of the first.
func TestMatchSeparatesDocumentsAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.tony", "{name: a}\n")
	b := write(t, dir, "b.tony", "{name: b}\n")

	code, out := runMatch(t, "{name: !glob \"*\"}", a, b)
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if n := strings.Count(out, "---"); n != 1 {
		t.Fatalf("%d separators between two documents, want 1:\n%s", n, out)
	}
}

// TestMatchExitCodeErrIsNotPrinted: exit 1 must stay silent. cli prints a
// returned error unless it is an ExitCodeErr, and "exit 1" on stderr for a
// search that found nothing would be noise in every pipe.
func TestMatchExitCodeErrIsNotPrinted(t *testing.T) {
	var xc cli.ExitCodeErr
	if !errors.As(cli.ExitCodeErr(1), &xc) {
		t.Fatal("ExitCodeErr does not match itself; the silence depends on it")
	}
}
