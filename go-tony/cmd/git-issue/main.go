// Command git-issue is a git-native issue tracker: issues live in the
// repository they describe, as git refs, so they clone, branch, work offline
// and sync with the code rather than beside it.
//
// An open issue is refs/issues/<xidr>, a closed one refs/closed/<xidr>, where
// <xidr> is a 20-character identifier assigned at creation. Each ref points at
// a commit chain whose tree holds the issue's description, metadata and
// discussion, so the issue's history is just git history. Installed on PATH,
// git dispatches "git issue ..." to this binary.
//
// Run "git issue -h" for the subcommand list. The subcommands themselves live
// in the commands package; the storage model and its accessors are documented
// in issuelib.
package main

import (
	"context"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/commands"
)

func main() {
	cli.MainContext(context.Background(), commands.Root())
}
