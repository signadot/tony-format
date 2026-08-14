package commands

import (
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/buildinfo"
)

// TestVersion_PrintsThisBuild: the version buildinfo reads is tested where it is
// computed; what is worth pinning here is that the subcommand is wired to it and
// says which command it is, since the answer usually arrives pasted into a bug
// report with no other clue as to which binary produced it.
func TestVersion_PrintsThisBuild(t *testing.T) {
	out := &strings.Builder{}
	cc := &cli.Context{Out: nopWriteCloser{out}, Err: nopWriteCloser{&strings.Builder{}}}

	cmd := VersionCommand(nil)
	if cmd.Hooks.Run == nil {
		t.Fatal("version command has no run hook")
	}
	if err := cmd.Hooks.Run(cc, nil); err != nil {
		t.Fatalf("version: %v", err)
	}

	if want := buildinfo.Line("git-issue") + "\n"; out.String() != want {
		t.Fatalf("version printed %q, want %q", out.String(), want)
	}
}
