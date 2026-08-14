package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
)

// runO runs the whole command tree, since the exit code is the root's answer and
// the config a subcommand reads is what MainCommand wires.
func runO(t *testing.T, args ...string) (code int, out string) {
	t.Helper()
	outBuf, errBuf := &strings.Builder{}, &strings.Builder{}
	cc := &cli.Context{Out: nopWC{outBuf}, Err: nopWC{errBuf}}
	cmd := MainCommand()
	err := cmd.Run(cc, args)
	return cmd.Exit(cc, err), outBuf.String()
}

func writeDoc(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const nestedDoc = `name: svc
items:
- {name: a, state: open}
- {name: b, state: closed}
- {name: c, state: open}
`

// TestListIf is the question this was built for: a list, filtered by a match.
//
// The path says where and the match says which, so the two halves stay separate
// and "each element" is just a path rather than a mode. That is why it reaches
// $.items[*] as easily as $[*], which a flag meaning "the elements of the
// top-level array" never could.
func TestListIf(t *testing.T) {
	dir := t.TempDir()
	list := writeDoc(t, dir, "list.tony", "- {name: a, state: open}\n- {name: b, state: closed}\n- {name: c, state: open}\n")
	nested := writeDoc(t, dir, "nested.tony", nestedDoc)

	for _, tc := range []struct {
		name     string
		args     []string
		wantCode int
		wantHas  []string
		wantNot  []string
	}{
		{
			name:     "a top-level list",
			args:     []string{"list", "-if", "{state: open}", "$[*]", list},
			wantCode: 0,
			wantHas:  []string{"name: a", "name: c"},
			wantNot:  []string{"name: b"},
		},
		{
			name:     "at depth",
			args:     []string{"list", "-if", "{state: open}", "$.items[*]", nested},
			wantCode: 0,
			wantHas:  []string{"name: a", "name: c"},
			wantNot:  []string{"name: b"},
		},
		{
			name:     "no -if keeps everything the path names",
			args:     []string{"list", "$[*]", list},
			wantCode: 0,
			wantHas:  []string{"name: a", "name: b", "name: c"},
		},
		{
			name:     "nothing matches",
			args:     []string{"list", "-if", "{state: zzz}", "$[*]", list},
			wantCode: 1,
		},
		{
			name:     "an unparseable match document is a fault, not an empty answer",
			args:     []string{"list", "-if", "{state: ", "$[*]", list},
			wantCode: 2,
		},
		{
			name:     "naming no input is a fault",
			args:     []string{"list", "-if", "{state: open}", "$[*]"},
			wantCode: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runO(t, tc.args...)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d (output %q)", code, tc.wantCode, out)
			}
			for _, want := range tc.wantHas {
				if !strings.Contains(out, want) {
					t.Errorf("output does not contain %q:\n%s", want, out)
				}
			}
			for _, not := range tc.wantNot {
				if strings.Contains(out, not) {
					t.Errorf("output contains %q, which does not match:\n%s", not, out)
				}
			}
		})
	}
}

// TestListIfEmptyIsStillAList: a query for a collection answers with a
// collection, so an empty result is written as []. The exit code, not the
// stream, is what says it was empty -- a consumer parsing the output gets
// something it can parse either way.
func TestListIfEmptyIsStillAList(t *testing.T) {
	dir := t.TempDir()
	list := writeDoc(t, dir, "list.tony", "- {name: a, state: open}\n")

	code, out := runO(t, "list", "-if", "{state: zzz}", "$[*]", list)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("wrote %q, want []", out)
	}
}

// TestGetIf: on a path naming one node, -if is a guard -- the node if it
// matches, nothing if it does not, and an exit code either way, so
// `o get -if ... && deploy` means what it reads as.
func TestGetIf(t *testing.T) {
	dir := t.TempDir()
	nested := writeDoc(t, dir, "nested.tony", nestedDoc)

	for _, tc := range []struct {
		name     string
		args     []string
		wantCode int
		wantOut  bool
	}{
		{"the node matches", []string{"get", "-if", "{state: open}", "$.items[0]", nested}, 0, true},
		{"the node does not match", []string{"get", "-if", "{state: open}", "$.items[1]", nested}, 1, false},
		{"the path names nothing", []string{"get", "-if", "{state: open}", "$.nope", nested}, 1, false},
		{"no -if, the path is the whole question", []string{"get", "$.items[1]", nested}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runO(t, tc.args...)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d", code, tc.wantCode)
			}
			if got := strings.TrimSpace(out) != ""; got != tc.wantOut {
				t.Fatalf("wrote something = %v, want %v (%q)", got, tc.wantOut, out)
			}
		})
	}
}

// TestIfAndIfFileAgree: -if-file is the same question read from a file, and
// giving both is a misuse rather than a silent preference for one.
func TestIfAndIfFileAgree(t *testing.T) {
	dir := t.TempDir()
	list := writeDoc(t, dir, "list.tony", "- {name: a, state: open}\n- {name: b, state: closed}\n")
	pat := writeDoc(t, dir, "pat.tony", "{state: open}\n")

	codeInline, outInline := runO(t, "list", "-if", "{state: open}", "$[*]", list)
	codeFile, outFile := runO(t, "list", "-if-file", pat, "$[*]", list)
	if codeInline != 0 || codeFile != 0 {
		t.Fatalf("exit codes %d and %d, want 0 and 0", codeInline, codeFile)
	}
	if outInline != outFile {
		t.Fatalf("-if and -if-file disagree:\n%s\n---\n%s", outInline, outFile)
	}

	if code, _ := runO(t, "list", "-if", "{state: open}", "-if-file", pat, "$[*]", list); code != 2 {
		t.Fatalf("giving both exits %d, want 2", code)
	}
}
