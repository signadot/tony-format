package commands

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func Push(args []string) error {
	pushAll := false
	var issueID int64

	// Parse arguments
	if len(args) == 0 {
		return fmt.Errorf("usage: git issue push <id> | --all")
	}

	if args[0] == "--all" {
		pushAll = true
	} else {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid issue ID: %s", args[0])
		}
		issueID = id
	}

	// Get remote name (default to origin)
	remote := "origin"
	if len(args) > 1 {
		remote = args[1]
	}

	// Verify remote exists
	checkCmd := exec.Command("git", "remote", "get-url", remote)
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("remote not found: %s", remote)
	}

	if pushAll {
		return pushAllIssues(remote)
	}
	return pushSingleIssue(remote, issueID)
}

func pushAllIssues(remote string) error {
	fmt.Printf("Pushing all issues to %s...\n", remote)

	// Push all issue refs
	refspecs := []string{
		"+refs/issues/*:refs/issues/*",
		"+refs/closed/*:refs/closed/*",
		"+refs/meta/issue-counter:refs/meta/issue-counter",
		"+refs/notes/issues:refs/notes/issues",
	}

	for _, refspec := range refspecs {
		cmd := exec.Command("git", "push", remote, refspec)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Don't fail if ref doesn't exist locally
			if !strings.Contains(string(output), "does not match any") {
				fmt.Printf("Warning: failed to push %s: %s\n", refspec, string(output))
			}
		}
	}

	fmt.Println("Done.")
	return nil
}

func pushSingleIssue(remote string, issueID int64) error {
	issueIDStr := fmt.Sprintf("%06d", issueID)

	// Try open issues first
	openRef := fmt.Sprintf("refs/issues/%s", issueIDStr)
	closedRef := fmt.Sprintf("refs/closed/%s", issueIDStr)

	// Check if issue exists locally
	checkCmd := exec.Command("git", "show-ref", openRef)
	err := checkCmd.Run()
	issueRef := openRef

	if err != nil {
		// Try closed
		checkCmd = exec.Command("git", "show-ref", closedRef)
		err = checkCmd.Run()
		if err != nil {
			return fmt.Errorf("issue not found: %06d", issueID)
		}
		issueRef = closedRef
	}

	fmt.Printf("Pushing issue #%06d to %s...\n", issueID, remote)

	// Push the issue ref
	pushCmd := exec.Command("git", "push", remote, fmt.Sprintf("+%s:%s", issueRef, issueRef))
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push issue: %s", string(output))
	}

	// Push issue counter
	counterCmd := exec.Command("git", "push", remote, "+refs/meta/issue-counter:refs/meta/issue-counter")
	_ = counterCmd.Run() // Ignore error if doesn't exist

	// Push notes for all commits referenced by this issue
	repo := &GitRepo{}
	issue, _, err := repo.ReadIssue(issueRef)
	if err == nil && len(issue.Commits) > 0 {
		// Push notes ref (contains all issue→commit mappings)
		notesCmd := exec.Command("git", "push", remote, "+refs/notes/issues:refs/notes/issues")
		_ = notesCmd.Run() // Ignore error
	}

	fmt.Printf("Pushed issue #%06d\n", issueID)
	return nil
}
