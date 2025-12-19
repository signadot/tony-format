package main

import (
	"fmt"
	"os"

	"github.com/signadot/tony-format/go-tony/cmd/git-issue/commands"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "create":
		if err := commands.Create(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := commands.List(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "show":
		if err := commands.Show(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "link":
		if err := commands.Link(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "close":
		if err := commands.Close(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "comment":
		if err := commands.Comment(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "attach":
		if err := commands.Attach(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "for-commit":
		if err := commands.ForCommit(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "relate":
		if err := commands.Relate(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "blocks":
		if err := commands.Blocks(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "duplicate":
		if err := commands.Duplicate(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "push":
		if err := commands.Push(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "pull":
		if err := commands.Pull(args); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `git-issue - Git-native issue tracker

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
`)
}
