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

func Close(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: git issue close <id> [--commit <sha>]")
	}

	idStr := args[0]
	var closingCommit *string

	// Parse optional --commit flag
	for i := 1; i < len(args); i++ {
		if args[i] == "--commit" && i+1 < len(args) {
			commit := args[i+1]
			// Verify commit exists
			verifyCmd := exec.Command("git", "rev-parse", "--verify", commit)
			verifyOut, err := verifyCmd.Output()
			if err != nil {
				return fmt.Errorf("commit not found: %s", commit)
			}
			sha := strings.TrimSpace(string(verifyOut))
			closingCommit = &sha
			break
		}
	}

	// Parse ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid issue ID: %s", idStr)
	}

	repo := &GitRepo{}

	// Read current issue
	ref := fmt.Sprintf("refs/issues/%06d", id)
	_, _, err = repo.ReadIssue(ref)
	if err != nil {
		return fmt.Errorf("issue not found or already closed: %06d", id)
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

	// Update status
	issue.Status = "closed"
	issue.Updated = time.Now()
	issue.ClosedBy = closingCommit

	// Convert back to tony
	newMetaNode := issue.MetaToNode()
	newMetaContent := encode.MustString(newMetaNode)

	// Update issue with closed status
	commitMsg := "close"
	if closingCommit != nil {
		commitMsg = fmt.Sprintf("close: closed by %s", (*closingCommit)[:7])
	}

	updates := map[string]string{
		"meta.tony": newMetaContent,
	}

	if err := repo.UpdateIssueCommit(id, commitMsg, updates); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// Move ref from refs/issues/ to refs/closed/
	currentRef := fmt.Sprintf("refs/issues/%06d", id)
	newRef := fmt.Sprintf("refs/closed/%06d", id)

	// Get current commit SHA
	showCmd := exec.Command("git", "show-ref", currentRef)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get current commit: %w", err)
	}
	commitSHA := strings.Fields(string(showOut))[0]

	// Create new ref
	updateCmd := exec.Command("git", "update-ref", newRef, commitSHA)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to create closed ref: %w", err)
	}

	// Delete old ref
	deleteCmd := exec.Command("git", "update-ref", "-d", currentRef)
	if err := deleteCmd.Run(); err != nil {
		return fmt.Errorf("failed to delete open ref: %w", err)
	}

	fmt.Printf("Closed issue #%06d\n", id)
	if closingCommit != nil {
		fmt.Printf("Closed by: %s\n", (*closingCommit)[:7])
	}

	return nil
}
