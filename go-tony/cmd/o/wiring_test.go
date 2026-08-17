package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
)

// The wiring is what every command has in common: -h is answered, a misuse exits
// 2 and says which word was wrong, and a fault exits 2 rather than 1. Each of
// those used to hold for some commands and not others, which is worse than
// holding for none: a reader who learns the rule from `o get` finds it does not
// apply to `o build`.

// runWired runs a whole command line through the tree and answers what the process
// would exit with, plus what it wrote where.
func runWired(t *testing.T, stdin string, args ...string) (code int, out, errOut string) {
	t.Helper()
	outBuf, errBuf := &strings.Builder{}, &strings.Builder{}
	cc := &cli.Context{
		Out: nopWC{outBuf},
		Err: nopWC{errBuf},
		In:  io.NopCloser(strings.NewReader(stdin)),
	}
	cmd := MainCommand()
	err := cmd.Run(cc, args)
	return cmd.Exit(cc, err), outBuf.String(), errBuf.String()
}

// Every command answers -h, on stdout, exiting 0. `o schema -h` used to exit 1
// with `unknown option: "h"`, because a group command registers no options of its
// own; `o version -h` printed the version, which is not what was asked.
func TestEveryCommandAnswersDashH(t *testing.T) {
	for _, args := range [][]string{
		{"-h"},
		{"get", "-h"}, {"list", "-h"}, {"match", "-h"}, {"patch", "-h"},
		{"view", "-h"}, {"eval", "-h"}, {"diff", "-h"}, {"dump", "-h"},
		{"load", "-h"}, {"build", "-h"}, {"docs", "-h"},
		{"help", "-h"}, {"completion", "-h"}, {"version", "-h"},
		{"schema", "-h"}, {"schema", "check", "-h"},
		{"system", "-h"},
		{"system", "logd", "-h"}, {"system", "logd", "serve", "-h"},
		{"system", "logd", "session", "-h"},
		{"system", "docd", "-h"}, {"system", "docd", "serve", "-h"},
		{"system", "docd", "mounts", "-h"},
		{"system", "up", "-h"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, errOut := runWired(t, "", args...)
			if code != 0 {
				t.Errorf("exit %d, want 0: %s", code, errOut)
			}
			if out == "" {
				t.Error("nothing written on stdout")
			}
			// Help is the usage text, which every command's begins with.
			if !strings.HasPrefix(out, "synopsis:") {
				t.Errorf("what was written is not help:\n%s", out)
			}
		})
	}
}

// `o version -h` is help, and `o version` is the version. The one that answers
// -h by doing its work is the one whose reader cannot ask.
func TestVersionAnswersBothQuestions(t *testing.T) {
	if code, out, _ := runWired(t, "", "version"); code != 0 || strings.HasPrefix(out, "synopsis:") {
		t.Errorf("o version: exit %d, wrote %q", code, out)
	}
	if code, out, _ := runWired(t, "", "version", "-h"); code != 0 || !strings.HasPrefix(out, "synopsis:") {
		t.Errorf("o version -h: exit %d, wrote %q", code, out)
	}
}

// A misuse is exit 2 wherever it is made -- at the root, in a group, or in a
// leaf. It used to be 1 for anything the library reported, which a pipe cannot
// tell from "the answer was nothing".
func TestMisuseIsAlwaysTwo(t *testing.T) {
	for _, args := range [][]string{
		{"-zzz"},
		{"-j", "-y", "get", "."},
		{"nosuchcommand"},
		{"get", "-zzz", "."},
		{"get"},
		{"build", "-zzz"},
		{"docs"},
		{"docs", "a", "b"},
		{"version", "extra"},
		{"completion"},
		{"completion", "nosuchshell"},
		{"help", "nosuchcommand"},
		{"schema", "nosuchsub"},
		{"schema", "check"},
		{"system", "nosuchsub"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if code, _, _ := runWired(t, "", args...); code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
		})
	}
}

// A fault is 2 in build and docs too. They exited 1, which in this tool means
// "the answer was nothing" -- so a build directory that does not exist read as a
// build that produced nothing.
func TestBuildAndDocsFaultsAreTwo(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	notADir := write(t, dir, "afile", "not a directory\n")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"build of a directory with no build file", []string{"build", missing}},
		// A file where the directory should be: docs makes the directory it is
		// given, so a path it cannot make is one already occupied.
		{"docs into a path that cannot be written", []string{"docs", notADir}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, _, _ := runWired(t, "", tc.args...); code != 2 {
				t.Errorf("exit %d, want 2", code)
			}
		})
	}
}

// schema check tells a document which does not satisfy the schema (1, an answer
// about the document) from a schema or file it could not read (2, a fault). A
// script which cannot tell them apart reports a missing schema as a bad manifest.
func TestSchemaCheckExitCodes(t *testing.T) {
	dir := t.TempDir()
	sch := write(t, dir, "s.tony", "define:\n  person:\n    name: .[string]\n    age: .[number]\naccept: .[person]\n")
	ok := write(t, dir, "ok.tony", "name: bill\nage: 3\n")
	bad := write(t, dir, "bad.tony", "name: bill\nage: notanint\n")
	missing := filepath.Join(dir, "nope.tony")

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"it satisfies the schema", []string{sch, ok}, 0},
		{"it does not", []string{sch, bad}, 1},
		{"one of several does not", []string{sch, ok, bad}, 1},
		{"no schema to check against", []string{missing, ok}, 2},
		{"no document to check", []string{sch, missing}, 2},
		{"a fault outranks an answer", []string{sch, bad, missing}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, _ := runWired(t, "", append([]string{"schema", "check"}, tc.args...)...)
			if code != tc.want {
				t.Errorf("exit %d, want %d", code, tc.want)
			}
		})
	}

	// Every failing document is reported, not just the first: one run says how
	// much is wrong.
	stream := "name: a\nage: x\n---\nname: b\nage: y\n"
	code, _, errOut := runWired(t, stream, "schema", "check", sch)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut, "doc 0") || !strings.Contains(errOut, "doc 1") {
		t.Errorf("only some failures reported:\n%s", errOut)
	}
}

// -o names a file, and a misuse in the subcommand used to leave it open: the
// usage-error path called os.Exit, which runs no deferred close. What the file
// holds afterwards is the test -- an unclosed one is short.
func TestOutputFileIsClosedOnAMisuse(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.tony")
	in := write(t, dir, "in.tony", "a: 1\n")

	// A run which works, to have something to compare against.
	if code, _, _ := runWired(t, "", "-o", out, "get", ".", in); code != 0 {
		t.Fatalf("exit %d writing %s", code, out)
	}
	good, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(good) == 0 {
		t.Fatal("nothing was written to the output file")
	}

	// And the same with a misuse after it: the code is 2, and getting there does
	// not skip the close.
	if code, _, _ := runWired(t, "", "-o", out, "get", "-zzz", "."); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output file after a misuse: %v", err)
	}
}
