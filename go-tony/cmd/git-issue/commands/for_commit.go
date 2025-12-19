package commands

import (
	"fmt"
	"os/exec"
	"strings"
)

func ForCommit(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: git issue for-commit <commit>")
	}

	commit := args[0]

	// Verify commit exists
	verifyCmd := exec.Command("git", "rev-parse", "--verify", commit)
	verifyOut, err := verifyCmd.Output()
	if err != nil {
		return fmt.Errorf("commit not found: %s", commit)
	}
	commitSHA := strings.TrimSpace(string(verifyOut))

	// Read git notes for this commit
	notesCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "show", commitSHA)
	notesOut, err := notesCmd.Output()
	if err != nil {
		// No notes means no linked issues
		fmt.Printf("No issues linked to commit %s\n", commitSHA[:7])
		return nil
	}

	// Parse issue IDs from notes (one per line)
	issueIDs := strings.Split(strings.TrimSpace(string(notesOut)), "\n")
	if len(issueIDs) == 0 || (len(issueIDs) == 1 && issueIDs[0] == "") {
		fmt.Printf("No issues linked to commit %s\n", commitSHA[:7])
		return nil
	}

	// Get commit message for context
	logCmd := exec.Command("git", "log", "-1", "--oneline", commitSHA)
	logOut, err := logCmd.Output()
	commitInfo := commitSHA[:7]
	if err == nil {
		commitInfo = strings.TrimSpace(string(logOut))
	}

	fmt.Printf("Issues linked to commit %s:\n\n", commitInfo)

	repo := &GitRepo{}

	// Show each linked issue
	for _, idStr := range issueIDs {
		idStr = strings.TrimSpace(idStr)
		if idStr == "" {
			continue
		}

		// Try open issues first
		ref := fmt.Sprintf("refs/issues/%s", idStr)
		issue, _, err := repo.ReadIssue(ref)
		if err != nil {
			// Try closed issues
			ref = fmt.Sprintf("refs/closed/%s", idStr)
			issue, _, err = repo.ReadIssue(ref)
		}

		if err != nil {
			fmt.Printf("  #%s (not found)\n", idStr)
			continue
		}

		status := issue.Status
		if strings.HasPrefix(ref, "refs/closed/") {
			status = "closed"
		}

		statusColor := ""
		if status == "open" {
			statusColor = "\033[32m" // Green
		} else {
			statusColor = "\033[90m" // Gray
		}
		resetColor := "\033[0m"

		fmt.Printf("  #%s %s[%s]%s %s\n", idStr, statusColor, status, resetColor, issue.Title)
	}

	return nil
}
