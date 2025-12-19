package commands

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func Relate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: git issue relate <id1> <id2>")
	}

	return addRelation(args[0], args[1], "related")
}

func Blocks(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: git issue blocks <id1> <id2>  (id1 blocks id2)")
	}

	return addRelation(args[0], args[1], "blocks")
}

func Duplicate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: git issue duplicate <id1> <id2>  (id1 duplicates id2)")
	}

	return addRelation(args[0], args[1], "duplicate")
}

func addRelation(id1Str, id2Str, relationType string) error {
	// Parse IDs
	id1, err := strconv.ParseInt(id1Str, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid issue ID: %s", id1Str)
	}

	id2, err := strconv.ParseInt(id2Str, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid issue ID: %s", id2Str)
	}

	repo := &GitRepo{}

	// Format issue IDs
	id1Formatted := fmt.Sprintf("%06d", id1)
	id2Formatted := fmt.Sprintf("%06d", id2)

	// Find and update first issue
	ref1 := fmt.Sprintf("refs/issues/%06d", id1)
	_, _, err = repo.ReadIssue(ref1)
	if err != nil {
		// Try closed
		ref1 = fmt.Sprintf("refs/closed/%06d", id1)
		_, _, err = repo.ReadIssue(ref1)
		if err != nil {
			return fmt.Errorf("issue not found: %06d", id1)
		}
	}

	// Verify second issue exists
	ref2 := fmt.Sprintf("refs/issues/%06d", id2)
	_, _, err = repo.ReadIssue(ref2)
	if err != nil {
		// Try closed
		ref2 = fmt.Sprintf("refs/closed/%06d", id2)
		_, _, err = repo.ReadIssue(ref2)
		if err != nil {
			return fmt.Errorf("issue not found: %06d", id2)
		}
	}

	// Read and update first issue
	metaCmd := exec.Command("git", "show", ref1+":meta.tony")
	metaOut, err := metaCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to read meta.tony: %w", err)
	}

	metaNode, err := parse.Parse(metaOut)
	if err != nil {
		return fmt.Errorf("failed to parse meta.tony: %w", err)
	}

	issue1, err := NodeToMeta(metaNode)
	if err != nil {
		return fmt.Errorf("failed to parse issue metadata: %w", err)
	}

	// Add relationship based on type
	var added bool
	switch relationType {
	case "related":
		if !contains(issue1.RelatedIssues, id2Formatted) {
			issue1.RelatedIssues = append(issue1.RelatedIssues, id2Formatted)
			added = true
		}
	case "blocks":
		if !contains(issue1.Blocks, id2Formatted) {
			issue1.Blocks = append(issue1.Blocks, id2Formatted)
			added = true
		}
	case "duplicate":
		if !contains(issue1.Duplicates, id2Formatted) {
			issue1.Duplicates = append(issue1.Duplicates, id2Formatted)
			added = true
		}
	}

	if !added {
		fmt.Printf("Issue #%06d already has this relationship with #%06d\n", id1, id2)
		return nil
	}

	issue1.Updated = time.Now()

	// Save updated meta
	newMetaNode := issue1.MetaToNode()
	newMetaContent := encode.MustString(newMetaNode)

	updates := map[string]string{
		"meta.tony": newMetaContent,
	}

	var commitMsg string
	switch relationType {
	case "related":
		commitMsg = fmt.Sprintf("relate: link to #%06d", id2)
	case "blocks":
		commitMsg = fmt.Sprintf("blocks: #%06d", id2)
	case "duplicate":
		commitMsg = fmt.Sprintf("duplicate: of #%06d", id2)
	}

	if err := repo.UpdateIssueCommitByRef(ref1, commitMsg, updates); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// For blocks relationship, add reciprocal blocked_by to second issue
	if relationType == "blocks" {
		metaCmd2 := exec.Command("git", "show", ref2+":meta.tony")
		metaOut2, err := metaCmd2.Output()
		if err == nil {
			metaNode2, err := parse.Parse(metaOut2)
			if err == nil {
				issue2, err := NodeToMeta(metaNode2)
				if err == nil && !contains(issue2.BlockedBy, id1Formatted) {
					issue2.BlockedBy = append(issue2.BlockedBy, id1Formatted)
					issue2.Updated = time.Now()

					newMetaNode2 := issue2.MetaToNode()
					newMetaContent2 := encode.MustString(newMetaNode2)

					updates2 := map[string]string{
						"meta.tony": newMetaContent2,
					}

					commitMsg2 := fmt.Sprintf("blocked-by: #%06d", id1)
					_ = repo.UpdateIssueCommitByRef(ref2, commitMsg2, updates2)
				}
			}
		}
	}

	switch relationType {
	case "related":
		fmt.Printf("Linked issue #%06d to #%06d\n", id1, id2)
	case "blocks":
		fmt.Printf("Issue #%06d now blocks #%06d\n", id1, id2)
	case "duplicate":
		fmt.Printf("Issue #%06d marked as duplicate of #%06d\n", id1, id2)
	}

	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
