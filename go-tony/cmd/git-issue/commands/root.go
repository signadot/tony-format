package commands

import (
	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

const usageText = `git-issue - Git-native issue tracker

Usage:
  git issue create <title>               Create new issue
  git issue list [--all]                 List issues (open by default)
  git issue show <id>                    Show issue details
  git issue link <id> <commit>           Link issue to commit
  git issue comment <id> [text]          Add comment to issue
  git issue attach <id> <path>           Attach file/directory to issue
  git issue for-commit <commit>          Show issues linked to commit
  git issue relate <id1> <id2>           Link two related issues
  git issue blocks <id1> <id2>           Issue id1 blocks id2
  git issue duplicate <id1> <id2>        Issue id1 duplicates id2
  git issue push <id> [remote]           Push issue to remote (default: origin)
  git issue push --all [remote]          Push all issues to remote
  git issue pull [remote]                Pull issues from remote (default: origin)
  git issue close <id> [--commit <sha>]  Close issue
  git issue reopen <id>                  Reopen a closed issue
  git issue export <id> [dir]            Export issue to directory
  git issue import <dir>                 Import issue from directory
  git issue label <id> <label>...        Add labels to issue
  git issue unlabel <id> <label>...      Remove labels from issue

Examples:
  git issue create "Implement streaming processor"
  git issue list
  git issue show 1
  git issue link 1 abc123def
  git issue comment 1 "This approach looks good"
  git issue attach 1 ./docs/design.md
  git issue relate 4 5        # Link issues 4 and 5
  git issue blocks 4 5         # Issue 4 blocks issue 5
  git issue push 4             # Push issue 4 to origin
  git issue push --all         # Push all issues to origin
  git issue pull               # Fetch issues from origin
  git issue for-commit HEAD
  git issue close 1 --commit def456
  git issue export 1 ./my-issue
  git issue import ./my-issue`

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
		)
}
