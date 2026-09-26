// Command git-issue is a git-native issue tracker: issues live in the
// repository they describe, as git refs, so they clone, branch, work offline
// and sync with the code rather than beside it.
//
// An open issue is refs/git-issues/v1/open/<xidr>, a closed one
// refs/git-issues/v1/closed/<xidr>, where <xidr> is a 20-character identifier
// minted at creation with no coordination, so clones never collide. Each ref
// points at a commit chain whose tree holds the issue's description, metadata
// and discussion, so the issue's history is just git history. Every clone is a
// complete tracker; push and pull merge what two clones changed and name what
// they cannot. Installed on PATH, git dispatches "git issue ..." to this binary,
// and "git issue mcp" serves the tracker to an agent's host over MCP.
//
// Run "git issue -h" for the subcommand list. The subcommands live in the
// commands package, what each does to an issue in ops, and the storage model
// and its accessors in issuelib. The docs directory of this module holds the
// user documentation.
package main

import (
	"context"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/commands"
)

func main() {
	cli.MainContext(context.Background(), commands.Root())
}
