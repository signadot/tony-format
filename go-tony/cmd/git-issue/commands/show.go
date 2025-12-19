package commands

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func Show(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: git issue show <id>")
	}

	idStr := args[0]
	// Parse and format to 3 digits
	idNum, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid issue ID: %s", idStr)
	}
	id := fmt.Sprintf("%06d", idNum)

	repo := &GitRepo{}

	// Try open issues first
	ref := "refs/issues/" + id
	issue, desc, err := repo.ReadIssue(ref)
	if err != nil {
		// Try closed issues
		ref = "refs/closed/" + id
		issue, desc, err = repo.ReadIssue(ref)
		if err != nil {
			return fmt.Errorf("issue not found: %s", id)
		}
	}

	// Print issue details
	status := issue.Status
	if strings.HasPrefix(ref, "refs/closed/") {
		status = "closed"
	}

	fmt.Printf("Issue #%06d [%s]\n", issue.ID, status)
	fmt.Printf("Ref: %s\n", ref)
	fmt.Println()

	// Print description
	fmt.Println(desc)
	fmt.Println()

	// Show linked commits if any
	if len(issue.Commits) > 0 {
		fmt.Println("Linked commits:")
		for _, commit := range issue.Commits {
			// Get commit message
			cmd := exec.Command("git", "log", "-1", "--oneline", commit)
			out, err := cmd.Output()
			if err == nil {
				fmt.Printf("  %s\n", strings.TrimSpace(string(out)))
			} else {
				fmt.Printf("  %s\n", commit)
			}
		}
		fmt.Println()
	}

	// Show linked branches if any
	if len(issue.Branches) > 0 {
		fmt.Println("Linked branches:")
		for _, branch := range issue.Branches {
			fmt.Printf("  %s\n", branch)
		}
		fmt.Println()
	}

	// Show related issues
	if len(issue.RelatedIssues) > 0 {
		fmt.Println("Related issues:")
		for _, relID := range issue.RelatedIssues {
			showRelatedIssue(repo, relID)
		}
		fmt.Println()
	}

	// Show blocks relationships
	if len(issue.Blocks) > 0 {
		fmt.Println("Blocks:")
		for _, blockID := range issue.Blocks {
			showRelatedIssue(repo, blockID)
		}
		fmt.Println()
	}

	// Show blocked_by relationships
	if len(issue.BlockedBy) > 0 {
		fmt.Println("Blocked by:")
		for _, blockerID := range issue.BlockedBy {
			showRelatedIssue(repo, blockerID)
		}
		fmt.Println()
	}

	// Show duplicates
	if len(issue.Duplicates) > 0 {
		fmt.Println("Duplicates:")
		for _, dupID := range issue.Duplicates {
			showRelatedIssue(repo, dupID)
		}
		fmt.Println()
	}

	// Show discussion comments and attachments
	// Check if discussion directory exists
	treeCmd := exec.Command("git", "cat-file", "-p", ref+"^{tree}")
	treeOut, err := treeCmd.Output()
	if err == nil {
		var discussionTreeHash string
		lines := strings.Split(string(treeOut), "\n")
		for _, line := range lines {
			if strings.Contains(line, "discussion") && strings.Contains(line, "tree") {
				parts := strings.Fields(line)
				if len(parts) >= 3 {
					discussionTreeHash = parts[2]
					break
				}
			}
		}

		if discussionTreeHash != "" {
			// Recursively list all files in discussion tree
			var comments []string
			var attachments []string

			var walkTree func(treeHash, prefix string)
			walkTree = func(treeHash, prefix string) {
				cmd := exec.Command("git", "cat-file", "-p", treeHash)
				out, err := cmd.Output()
				if err != nil {
					return
				}

				lines := strings.Split(string(out), "\n")
				for _, line := range lines {
					if strings.TrimSpace(line) == "" {
						continue
					}
					parts := strings.Fields(line)
					if len(parts) < 4 {
						continue
					}
					typ, hash := parts[1], parts[2]
					name := strings.Join(parts[3:], " ")
					path := prefix + name

					if typ == "tree" {
						walkTree(hash, path+"/")
					} else if typ == "blob" {
						fullPath := "discussion/" + path
						if strings.HasSuffix(name, ".md") && !strings.Contains(path, "/") {
							comments = append(comments, fullPath)
						} else {
							attachments = append(attachments, fullPath)
						}
					}
				}
			}

			walkTree(discussionTreeHash, "")

			// Show comments
			if len(comments) > 0 {
				fmt.Println("Discussion:")
				fmt.Println()
				for _, file := range comments {
					contentCmd := exec.Command("git", "show", ref+":"+file)
					content, err := contentCmd.Output()
					if err == nil {
						fmt.Printf("--- %s ---\n", file)
						fmt.Print(string(content))
						fmt.Println()
					}
				}
			}

			// Show attachments
			if len(attachments) > 0 {
				fmt.Println("Attachments:")
				for _, file := range attachments {
					fmt.Printf("  %s\n", file)
				}
				fmt.Println()
			}
		}
	}

	return nil
}

func showRelatedIssue(repo *GitRepo, issueID string) {
	// Try open issues first
	ref := fmt.Sprintf("refs/issues/%s", issueID)
	relIssue, _, err := repo.ReadIssue(ref)
	if err != nil {
		// Try closed issues
		ref = fmt.Sprintf("refs/closed/%s", issueID)
		relIssue, _, err = repo.ReadIssue(ref)
	}

	if err != nil {
		fmt.Printf("  #%s (not found)\n", issueID)
		return
	}

	status := relIssue.Status
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

	fmt.Printf("  #%s %s[%s]%s %s\n", issueID, statusColor, status, resetColor, relIssue.Title)
}
