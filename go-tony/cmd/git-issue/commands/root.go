// Package commands implements the git-issue subcommands.
//
// Every subcommand is a func(issuelib.Store) *cli.Command: the store is passed
// in rather than reached for, so Root can share one store across the whole tree
// and a test can build the same commands against a store pointed at a scratch
// repository.
//
// Subcommands fall into a few groups:
//
//   - create, close, reopen, comment, attach, label, unlabel -- edit an issue
//   - list, show, for-commit -- read issues
//   - link, relate, blocks, duplicate -- record relationships, between an issue
//     and a commit or between two issues
//   - push, pull -- sync issue refs with a remote
//   - export, import -- move an issue between a repository and a directory
//   - serve -- a read-only web view of the repository's issues
//   - migrate, migrate-comments -- one-shot upgrades of on-disk layout
//
// Sync is deliberately blunt: push and pull both force refspecs and there is no
// merge step, so the last writer of a given issue wins. Everything downstream of
// that -- serve being read-only, comments being stored under content-addressed
// names that two clones cannot collide on -- follows from it.
package commands

import (
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

const usageText = `git-issue - Git-native issue tracker

Issues are git refs in this repository: refs/issues/<id> while open,
refs/closed/<id> once closed. An <id> is a 20-character XIDR, and every
command below takes any unambiguous prefix of one.

Usage:
  git issue create <title> [--body <text>]  Create new issue ($EDITOR or stdin)
  git issue list [--all] [--label <label>]  List issues (open by default)
  git issue show <id>                       Show issue details
  git issue link <id> <commit>              Link issue to commit
  git issue comment <id> [text]             Add comment to issue
  git issue attach <id> <path>              Attach file/directory to issue
  git issue for-commit <commit>             Show issues linked to commit
  git issue label <id> <label>...           Add labels to issue
  git issue unlabel <id> <label>...         Remove labels from issue
  git issue relate <id1> <id2>              Link two related issues
  git issue blocks <id1> <id2>              Issue id1 blocks id2
  git issue duplicate <id1> <id2>           Issue id1 duplicates id2
  git issue close <id> [--commit <sha>]     Close issue
  git issue reopen <id>                     Reopen a closed issue
  git issue push <id> [remote]              Push issue to remote (default: origin)
  git issue push --all [remote]             Push all issues to remote
  git issue pull [remote]                   Pull issues from remote (default: origin)
  git issue export <id> [dir]               Export issue to directory
  git issue import [--force] <dir>          Import issue from directory
  git issue serve [--addr <addr>]           Read-only web view (default localhost:8080)
  git issue migrate [--dry-run]             Migrate issues from numeric IDs to XIDs
  git issue migrate-comments [--apply]      Rename comments to collision-free names

Examples:
  git issue create "Implement streaming processor"
  git issue list --label bug
  git issue show j2dz          # prefix of j2dzt7xph12kswa9esn0
  git issue link j2dz HEAD
  git issue comment j2dz "This approach looks good"
  git issue attach j2dz ./docs/design.md
  git issue label j2dz bug urgent
  git issue relate j2dz 4f1c   # link two related issues
  git issue blocks j2dz 4f1c   # issue j2dz blocks 4f1c
  git issue push j2dz          # push one issue to origin
  git issue push --all         # push all issues to origin
  git issue pull               # fetch issues from origin
  git issue for-commit HEAD
  git issue close j2dz --commit def456
  git issue export j2dz ./my-issue
  git issue import ./my-issue
  git issue serve              # browse issues at http://localhost:8080/`

// Root returns the root command for git-issue.
func Root() *cli.Command {
	store := issuelib.NewGitStore()

	return cli.NewCommand("git-issue").
		WithSynopsis("git-issue - Git-native issue tracker").
		WithDescription(usageText).
		WithSubs(
			CreateCommand(store),
			ListCommand(store),
			ShowCommand(store),
			LinkCommand(store),
			CommentCommand(store),
			AttachCommand(store),
			ForCommitCommand(store),
			RelateCommand(store),
			BlocksCommand(store),
			DuplicateCommand(store),
			PushCommand(store),
			PullCommand(store),
			CloseCommand(store),
			ReopenCommand(store),
			ExportCommand(store),
			ImportCommand(store),
			LabelCommand(store),
			UnlabelCommand(store),
			MigrateCommand(store),
			MigrateCommentsCommand(store),
			ServeCommand(store),
		)
}
