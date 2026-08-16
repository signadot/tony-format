package main

import (
	"io"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
)

// runOIn runs a command through the whole tree and answers the exit code and what
// it wrote, with standard input supplied: a command given no file reads it, and a
// stream of documents is the thing under test here.
func runOIn(t *testing.T, stdin string, args ...string) (code int, out string) {
	t.Helper()
	outBuf, errBuf := &strings.Builder{}, &strings.Builder{}
	cc := &cli.Context{
		Out: nopWC{outBuf},
		Err: nopWC{errBuf},
		In:  io.NopCloser(strings.NewReader(stdin)),
	}
	cmd := MainCommand()
	err := cmd.Run(cc, args)
	return cmd.Exit(cc, err), outBuf.String()
}

// An input holds a stream of documents, and every command that reads one reads it
// that way -- which is what makes the output of one command the input of the next.
//
// get and list read the first document and faulted on the separator before the
// second ("imbalanced document: trailing material TDocSep"), so `o` could not
// consume its own output: `o get .a f1 f2 | o get .a` was an error, and every
// pipeline of more than one document had to be written some other way.
func TestCommandsReadAStreamOfDocuments(t *testing.T) {
	const stream = "{a: 1, b: {c: 2}}\n---\n{a: 2, b: {c: 3}}\n"

	for _, tc := range []struct {
		name string
		args []string
		want string
		code int
	}{
		{
			name: "get answers for every document",
			args: []string{"get", ".a"},
			want: "1\n---\n# from -\n2\n",
		},
		{
			name: "list gathers every document into one list",
			args: []string{"list", ".a"},
			want: "- 1\n- 2\n",
		},
		{
			name: "match already did, and still does",
			args: []string{"match", "{a: 2}"},
			want: "{\n  a: 2\n  b: {\n    c: 3\n  }\n}\n",
		},
		{
			name: "patch applies to every document",
			args: []string{"patch", "{d: 4}"},
			want: "{\n  a: 1\n  b: {\n    c: 2\n  }\n  d: 4\n}\n---\n{\n  a: 2\n  b: {\n    c: 3\n  }\n  d: 4\n}\n",
		},
		{
			name: "a query naming nothing in either document is empty, not a fault",
			args: []string{"get", ".nope"},
			code: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runOIn(t, stream, tc.args...)
			if code != tc.code {
				t.Errorf("exit %d, want %d (output %q)", code, tc.code, out)
			}
			if tc.want != "" && out != tc.want {
				t.Errorf("got  %q\nwant %q", out, tc.want)
			}
		})
	}
}

// What a command writes, another command reads. The separator is the contract, so
// this walks the pipelines rather than trusting that it is.
func TestOutputIsInput(t *testing.T) {
	const stream = "{a: 1, b: {c: 2}}\n---\n{a: 2, b: {c: 3}}\n"

	for _, tc := range []struct {
		name        string
		first, then []string
		want        string
	}{
		{
			name:  "get | get",
			first: []string{"get", ".b"},
			then:  []string{"get", ".c"},
			want:  "2\n---\n# from -\n3\n",
		},
		{
			name:  "match | get",
			first: []string{"match", "{a: 2}"},
			then:  []string{"get", ".a"},
			want:  "2\n",
		},
		{
			name:  "get | patch",
			first: []string{"get", ".b"},
			then:  []string{"patch", "{d: 4}"},
			want:  "{\n  c: 2\n  d: 4\n}\n---\n{\n  c: 3\n  d: 4\n}\n",
		},
		{
			name:  "patch | list",
			first: []string{"patch", "{d: 4}"},
			then:  []string{"list", ".d"},
			want:  "- 4\n- 4\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, mid := runOIn(t, stream, tc.first...)
			if code != 0 {
				t.Fatalf("%v exited %d", tc.first, code)
			}
			code, out := runOIn(t, mid, tc.then...)
			if code != 0 {
				t.Fatalf("%v exited %d reading %q", tc.then, code, mid)
			}
			if out != tc.want {
				t.Errorf("got  %q\nwant %q", out, tc.want)
			}
		})
	}
}

// patch reads its documents the way the others do: from the files named, or from
// standard input when none is. It used to require exactly one file and no standard
// input, while its own synopsis said `[files]`, so the obvious pipeline answered
// with a usage error -- and reported it as exit 1, which is the code for an answer.
func TestPatchTakesItsDocumentsLikeTheRest(t *testing.T) {
	dir := t.TempDir()
	f1 := write(t, dir, "f1.tony", "{a: 1}\n")
	f2 := write(t, dir, "f2.tony", "{a: 9}\n")

	t.Run("no file reads standard input", func(t *testing.T) {
		code, out := runOIn(t, "{a: 1}\n", "patch", "{b: 2}")
		if code != 0 {
			t.Fatalf("exit %d, want 0", code)
		}
		if want := "{\n  a: 1\n  b: 2\n}\n"; out != want {
			t.Errorf("got %q, want %q", out, want)
		}
	})

	t.Run("several files, separated so they read back", func(t *testing.T) {
		code, out := runOIn(t, "", "patch", "{b: 2}", f1, f2)
		if code != 0 {
			t.Fatalf("exit %d, want 0", code)
		}
		if want := "{\n  a: 1\n  b: 2\n}\n---\n{\n  a: 9\n  b: 2\n}\n"; out != want {
			t.Errorf("got %q, want %q", out, want)
		}
	})

	t.Run("a patch object and nothing else is a misuse, which is exit 2", func(t *testing.T) {
		if code, _ := runOIn(t, "", "patch"); code != 2 {
			t.Errorf("exit %d, want 2", code)
		}
	})
}

// A missing operand is standard input -- grep does not make you write `-`, and
// neither does anything here. diff was the one command that did: it wanted two
// operands and had no way to be given one but the dash.
func TestAMissingOperandIsStandardInput(t *testing.T) {
	dir := t.TempDir()
	f1 := write(t, dir, "f1.tony", "{a: 1}\n")

	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
		want  int
	}{
		{"diff, one operand, the same document", "{a: 1}\n", []string{"diff", f1}, 0},
		{"diff, one operand, a different one", "{a: 9}\n", []string{"diff", f1}, 1},
		{"diff, the dash still says which side", "{a: 9}\n", []string{"diff", "-", f1}, 1},
		{"diff, no operand at all, since both sides cannot be stdin", "{a: 1}\n", []string{"diff"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, out := runOIn(t, tc.stdin, tc.args...); code != tc.want {
				t.Errorf("exit %d, want %d (output %q)", code, tc.want, out)
			}
		})
	}
}

// 0 is an answer, 1 is an answer, 2 is a fault. get, list and match said so; the
// rest reported every failure as 1, so an unreadable file and an empty result were
// the same code -- and for diff, a fault and "the documents differ" were.
func TestFaultsExitTwo(t *testing.T) {
	dir := t.TempDir()
	f1 := write(t, dir, "f1.tony", "{a: 1}\n")
	f2 := write(t, dir, "f2.tony", "{a: 9}\n")
	missing := dir + "/nope.tony"

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"view an unreadable file", []string{"view", missing}, 2},
		{"dump an unreadable file", []string{"dump", missing}, 2},
		{"load an unreadable file", []string{"load", missing}, 2},
		{"eval an unreadable file", []string{"eval", missing}, 2},
		{"patch an unreadable file", []string{"patch", "{b: 2}", missing}, 2},
		{"diff an unreadable file", []string{"diff", missing, f1}, 2},
		{"diff, documents that differ", []string{"diff", f1, f2}, 1},
		{"diff, documents that do not", []string{"diff", f1, f1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code, out := runOIn(t, "", tc.args...); code != tc.want {
				t.Errorf("exit %d, want %d (output %q)", code, tc.want, out)
			}
		})
	}
}

// Asking what a command does is not a misuse of it. Every spelling used to print
// the help and then say `usage error: unknown option: "help"` on stderr, exit 1.
func TestHelpIsAnsweredNotRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"the long flag", []string{"--help"}, "commands:"},
		{"the short flag", []string{"-h"}, "commands:"},
		{"the word", []string{"help"}, "commands:"},
		{"for one command", []string{"help", "get"}, "get [opts] <kpath>"},
		{"the flag after a command", []string{"get", "-h"}, "get [opts] <kpath>"},
		{"nothing at all", nil, "please choose a command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runOIn(t, "", tc.args...)
			if code != 0 {
				t.Errorf("exit %d, want 0", code)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("help does not mention %q:\n%s", tc.want, out)
			}
		})
	}
}

// A misuse is still a misuse: exit 2, and say which word was wrong.
func TestAMistypedCommandIsAFaultWithASuggestion(t *testing.T) {
	code, _ := runOIn(t, "", "gett", ".a")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if got := nearestCommand(MainCommand(), "gett"); got != "get" {
		t.Errorf("nearest to %q is %q, want get", "gett", got)
	}
	if got := nearestCommand(MainCommand(), "mtch"); got != "match" {
		t.Errorf("nearest to %q is %q, want match", "mtch", got)
	}
}

// The completion scripts are generated from the tree, so they cannot name a
// command o does not have -- but they can be malformed, which the shells would
// find out at source time and the reader at completion time.
func TestCompletionScriptsNameEveryCommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			code, out := runOIn(t, "", "completion", shell)
			if code != 0 {
				t.Fatalf("exit %d, want 0", code)
			}
			for _, c := range MainCommand().Children {
				if !strings.Contains(out, c.Name) {
					t.Errorf("the %s script does not name %q", shell, c.Name)
				}
			}
		})
	}
	if code, _ := runOIn(t, "", "completion", "tcsh"); code != 2 {
		t.Errorf("a shell with no completion exits %d, want 2", code)
	}
	if code, _ := runOIn(t, "", "completion"); code != 2 {
		t.Errorf("no shell at all exits %d, want 2", code)
	}
}

// get and list ask their question in kpath, the syntax the rest of the system
// uses -- and which the same binary already used in `!at(...)`. They asked it in
// objpath, whose leading $ said nothing and whose grammar could not name a keyed
// or a sparse element at all.
func TestQueryPathsAreKPaths(t *testing.T) {
	dir := t.TempDir()
	plain := write(t, dir, "plain.tony", "{a: 1, b: {c: 2}}\n")
	keyed := write(t, dir, "keyed.tony", "items: !key(sku)\n- sku: WIDGET\n  qty: 5\n- sku: BOLT\n  qty: 9\n")

	for _, tc := range []struct {
		name string
		args []string
		want string
		code int
	}{
		// The papercut: the natural spelling was the one that failed.
		{"a bare field", []string{"get", "a", plain}, "1\n", 0},
		{"a leading dot", []string{"get", ".a", plain}, "1\n", 0},
		{"nested", []string{"get", "b.c", plain}, "2\n", 0},
		// What objpath could not say at all.
		{"a keyed element", []string{"get", "items(WIDGET).qty", keyed}, "5\n", 0},
		{"a wildcard, in list where it belongs", []string{"list", "items[*].sku", keyed}, "- WIDGET\n- BOLT\n", 0},
		// The old spelling still runs, rather than reading $ as a field name and
		// answering "nothing found", which is the wrong answer worse than an error.
		{"the legacy $ prefix", []string{"get", "$.a", plain}, "1\n", 0},
		{"the legacy root", []string{"get", "$", plain}, "{\n  a: 1\n  b: {\n    c: 2\n  }\n}\n", 0},
		// And the one thing it had that kpath does not says so.
		{"any-depth, which kpath cannot spell", []string{"list", "$...c", plain}, "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runOIn(t, "", tc.args...)
			if code != tc.code {
				t.Errorf("exit %d, want %d (output %q)", code, tc.code, out)
			}
			if tc.want != "" && out != tc.want {
				t.Errorf("got %q, want %q", out, tc.want)
			}
		})
	}
}
