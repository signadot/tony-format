package commands

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func Link(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: git issue link <id> <commit>")
	}

	idStr := args[0]
	commit := args[1]

	// Parse ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid issue ID: %s", idStr)
	}

	// Verify commit exists
	verifyCmd := exec.Command("git", "rev-parse", "--verify", commit)
	verifyOut, err := verifyCmd.Output()
	if err != nil {
		return fmt.Errorf("commit not found: %s", commit)
	}
	commitSHA := strings.TrimSpace(string(verifyOut))

	repo := &GitRepo{}

	// Read current issue
	ref := fmt.Sprintf("refs/issues/%06d", id)
	_, _, err = repo.ReadIssue(ref)
	if err != nil {
		return fmt.Errorf("issue not found: %06d", id)
	}

	// Read current meta.tony
	metaCmd := exec.Command("git", "show", ref+":meta.tony")
	metaOut, err := metaCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to read meta.tony: %w", err)
	}

	// Parse current meta
	metaNode, err := parse.Parse(metaOut)
	if err != nil {
		return fmt.Errorf("failed to parse meta.tony: %w", err)
	}

	issue, err := NodeToMeta(metaNode)
	if err != nil {
		return fmt.Errorf("failed to parse issue metadata: %w", err)
	}

	// Add commit to list if not already there
	found := false
	for _, c := range issue.Commits {
		if c == commitSHA {
			found = true
			break
		}
	}

	if !found {
		issue.Commits = append(issue.Commits, commitSHA)
		issue.Updated = time.Now()

		// Convert back to tony
		newMetaNode := issue.MetaToNode()
		newMetaContent := encode.MustString(newMetaNode)

		// Update issue with new commit
		commitMsg := fmt.Sprintf("link: %s", commitSHA[:7])
		updates := map[string]string{
			"meta.tony": newMetaContent,
		}

		if err := repo.UpdateIssueCommit(id, commitMsg, updates); err != nil {
			return fmt.Errorf("failed to update issue: %w", err)
		}
	}

	// Add to git notes (reverse index) - always do this to ensure reverse index is consistent
	// First check if note already contains this issue
	checkCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "show", commitSHA)
	checkOut, checkErr := checkCmd.Output()

	issueIDStr := fmt.Sprintf("%06d", id)
	needsUpdate := true

	if checkErr == nil {
		// Note exists, check if it already contains this issue
		existingIssues := strings.Split(strings.TrimSpace(string(checkOut)), "\n")
		for _, existing := range existingIssues {
			if strings.TrimSpace(existing) == issueIDStr {
				needsUpdate = false
				break
			}
		}
	}

	if needsUpdate {
		if checkErr == nil {
			// Append to existing note
			appendCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "append", "-m", issueIDStr, commitSHA)
			_ = appendCmd.Run() // Ignore error
		} else {
			// Create new note
			addCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "add", "-m", issueIDStr, commitSHA)
			_ = addCmd.Run() // Ignore error
		}
	}

	fmt.Printf("Linked issue #%06d to commit %s\n", id, commitSHA[:7])

	return nil
}
