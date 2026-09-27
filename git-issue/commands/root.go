// Package commands implements the git-issue subcommands.
//
// Every subcommand is a func(issuelib.Store) *cli.Command: the store is passed
// in rather than reached for, so Root can share one store across the whole tree
// and a test can build the same commands against a store pointed at a scratch
// repository.
//
// Subcommands fall into a few groups:
//
//   - create, edit, close, reopen, comment, attach, label, unlabel -- change an issue
//   - list, show, for-commit -- read issues
//   - link, relate, blocks, duplicate -- record relationships, between an issue
//     and a commit or between two issues
//   - push, pull -- sync issue refs with a remote
//   - ext add, fetch, refresh, remove, list -- another repository's issue,
//     mirrored here read-only so a relation to it resolves from this repository
//   - export, import -- copy an issue's tree out to a directory, and write an
//     edited copy back onto the issue's ref
//   - serve -- a read-only web view of the repository's issues; with -watch
//     it pulls the remote and open pages reload as issues change
//     (serve_watch.go)
//   - mcp -- the tracker as an MCP server over stdio, for an agent's host to
//     start: one repository or several, issue_* and repo_* tools, issue://
//     resources a host can subscribe to, and issue_watch for an agent whose
//     host does not (mcp.go, mcp_tools.go, mcp_workspace.go, mcp_resources.go,
//     mcp_watch.go)
//   - watch -- a line for each issue that changes, until stopped: the MCP
//     server's watch for an agent woken by a command's output (watch.go)
//   - migrate, migrate-comments -- one-shot upgrades of on-disk layout
//
// Each command parses what it was given and calls the ops package, which is
// where an operation's meaning lives; the CLI and the MCP server are two front
// ends over the same functions.
//
// Sync decides before it writes. Both directions fetch what the remote holds
// into tracking refs, take one verdict per issue from the two sides' commits,
// and act on it: an issue whose chain one side carries is sent or taken, and one
// neither side's chain carries is left alone and named, since taking either would
// drop the other's work. --force is how a person decides such an issue, and what
// it overwrites stays in the ref's reflog. Every write to the remote carries a
// lease on what the fetch saw, so a push cannot land on top of one made since.
//
// The namespace follows the same decision: an issue is open or closed, never
// both, on either side. A push makes the remote hold exactly one ref for an
// issue, which is how a close is mirrored, and it is refused rather than
// mirrored when the remote's tip is not one this clone carries -- a remote
// holding an id in both namespaces cannot be read for a status at all, and a
// close that deleted someone else's reopen would be a loss.
package commands

import "github.com/scott-cotton/cli"

const usageText = `git-issue - Git-native issue tracker

Issues are git refs in this repository: refs/git-issues/v1/open/<id> while open,
refs/git-issues/v1/closed/<id> once closed. An <id> is a 20-character XIDR, and every
command below takes any unambiguous prefix of one.

A command runs on the repository it is run in, or the one --repo names, or the
one of ~/.config/git-issue.tony's that holds the issue it is given.

Usage:
  git issue create <title> [--body <text>]  Create new issue ($EDITOR or stdin)
  git issue list [--all] [--label <label>]  List issues (open by default)
  git issue show <id>                       Show issue details
  git issue link <id> <commit>              Link issue to commit
  git issue edit <id> [--title t] [--body b] Change an issue's title or body ($EDITOR or stdin)
  git issue comment <id> [text]             Add comment to issue
  git issue attach <id> <path>              Attach file/directory to issue
  git issue for-commit <commit>             Show issues linked to commit
  git issue label <id> <label>...           Add labels to issue (key=value replaces the key's value)
  git issue unlabel <id> <label>...         Remove labels from issue (a bare key removes any value)
  git issue relate <id1> <id2>              Link two related issues
  git issue blocks <id1> <id2>              Issue id1 blocks id2
  git issue duplicate <id1> <id2>           Issue id1 duplicates id2
  git issue close <id> [--commit <sha>]     Close issue
  git issue reopen <id>                     Reopen a closed issue
  git issue push [--force] [--dry-run] <id> [remote]   Push issue to remote (default: origin)
  git issue push --all [--force] [--dry-run] [remote]  Push all issues to remote
  git issue pull [--force] [--dry-run] [remote]        Pull issues from remote (default: origin)
  git issue export <id> [dir]               Export issue to directory
  git issue import [--force] <dir>          Import issue from directory
  git issue ext add <source> <url>          Name another repository, to mirror its issues from
  git issue ext fetch <source> <id>         Mirror one of its issues here, read-only
  git issue ext refresh [<source>]          Bring mirrors up to their sources
  git issue ext remove <id>                 Drop a mirror and the relations naming it
  git issue serve [--addr <addr>] [-watch]  Read-only web view (default localhost:8080); -watch pulls, and pages reload
  git issue mcp [-C <dir>]                  Serve the tracker to an agent's host over MCP (stdin/stdout)
  git issue watch [--label <l>] [--local] [<id>...]  Pull origin; print each issue that changes, until stopped
  git issue migrate [--dry-run]             Migrate issues from numeric IDs to XIDs
  git issue migrate-comments [--apply]      Rename comments to collision-free names
  git issue version                         Print the version of git-issue

Examples:
  git issue create "Implement streaming processor"
  git issue list --label bug
  git issue show j2dz          # prefix of j2dzt7xph12kswa9esn0
  git issue link j2dz HEAD
  git issue comment j2dz "This approach looks good"
  git issue attach j2dz ./docs/design.md
  git issue label j2dz bug urgent
  git issue label j2dz severity=high  # a key holds one value; git-issue-* keys are reserved
  git issue relate j2dz 4f1c   # link two related issues
  git issue blocks j2dz 4f1c   # issue j2dz blocks 4f1c
  git issue push j2dz          # push one issue to origin
  git issue push --all         # push all issues to origin
  git issue pull               # fetch issues from origin
  git issue pull --dry-run     # say what a pull would do, and write nothing
  git issue for-commit HEAD
  git issue close j2dz --commit def456
  git issue export j2dz ./my-issue
  git issue import ./my-issue
  git issue serve              # browse issues at http://localhost:8080/`

// Root returns the root command for git-issue. The commands share one store,
// and where they are dispatched it is pointed at the repository the command
// runs on, by what the command's arguments name (set.go). mcp has its working
// set, and version needs no repository.
func Root() *cli.Command {
	d := newDispatch()
	store := d.target

	return cli.NewCommand("git-issue").
		WithSynopsis("git-issue - Git-native issue tracker").
		WithDescription(usageText).
		WithOpts(d.repoOpt()).
		WithSubs(
			d.in(CreateCommand(store), aRepository),
			d.in(ListCommand(store), everyRepository),
			d.in(ShowCommand(store), anIssue),
			d.in(EditCommand(store), anIssue),
			d.in(LinkCommand(store), anIssue),
			d.in(CommentCommand(store), anIssue),
			d.in(AttachCommand(store), anIssue),
			d.in(ForCommitCommand(store), aRepository),
			d.in(RelateCommand(store), anIssue),
			d.in(BlocksCommand(store), anIssue),
			d.in(DuplicateCommand(store), anIssue),
			d.in(PushCommand(store), anIssueOrNone),
			d.in(PullCommand(store), eachRepository),
			d.in(CloseCommand(store), anIssue),
			d.in(ReopenCommand(store), anIssue),
			d.in(ExportCommand(store), anIssue),
			d.in(ImportCommand(store), aRepository),
			d.in(LabelCommand(store), anIssue),
			d.in(UnlabelCommand(store), anIssue),
			d.in(MigrateCommand(store), theWorkingDirectory),
			d.in(MigrateCommentsCommand(store), theWorkingDirectory),
			d.sub(d.sub(d.sub(d.in(ExtCommand(store), aRepository), "remove", anIssue),
				"list", eachRepository), "refresh", eachRepository),
			d.in(ServeCommand(store), aRepository),
			MCPCommand(store),
			d.in(WatchCommand(store), everyRepository),
			VersionCommand(store),
		)
}
