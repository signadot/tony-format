package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func List(args []string) error {
	includeAll := false
	for _, arg := range args {
		if arg == "--all" || arg == "-a" {
			includeAll = true
		}
	}

	repo := &GitRepo{}

	refs, err := repo.ListIssueRefs(includeAll)
	if err != nil {
		return fmt.Errorf("failed to list issues: %w", err)
	}

	if len(refs) == 0 {
		fmt.Println("No issues found")
		return nil
	}

	// Read all issues
	type issueWithRef struct {
		ref   string
		issue *Issue
	}

	var issues []issueWithRef
	for _, ref := range refs {
		issue, _, err := repo.ReadIssue(ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read %s: %v\n", ref, err)
			continue
		}
		issues = append(issues, issueWithRef{ref: ref, issue: issue})
	}

	// Sort by ID (descending)
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].issue.ID > issues[j].issue.ID
	})

	// Print issues
	for _, iwr := range issues {
		issue := iwr.issue
		ref := iwr.ref

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

		fmt.Printf("#%06d %s[%s]%s %s\n",
			issue.ID,
			statusColor,
			status,
			resetColor,
			issue.Title,
		)
	}

	return nil
}
