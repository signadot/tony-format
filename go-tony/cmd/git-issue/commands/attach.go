package commands

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func Attach(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: git issue attach <id> <path>")
	}

	idStr := args[0]
	attachPath := args[1]

	// Parse ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid issue ID: %s", idStr)
	}

	// Check if path exists
	info, err := os.Stat(attachPath)
	if err != nil {
		return fmt.Errorf("path not found: %s", attachPath)
	}

	repo := &GitRepo{}

	// Try open issues first
	ref := fmt.Sprintf("refs/issues/%06d", id)
	_, _, err = repo.ReadIssue(ref)
	if err != nil {
		// Try closed issues
		ref = fmt.Sprintf("refs/closed/%06d", id)
		_, _, err = repo.ReadIssue(ref)
		if err != nil {
			return fmt.Errorf("issue not found: %06d", id)
		}
	}

	// Collect files to attach
	updates := make(map[string]string)
	baseName := filepath.Base(attachPath)

	if info.IsDir() {
		// Attach directory recursively
		err = filepath.WalkDir(attachPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				content, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("failed to read %s: %w", path, err)
				}
				// Preserve structure under discussion/files/basename/
				relPath, _ := filepath.Rel(attachPath, path)
				discussionPath := filepath.Join("discussion/files", baseName, relPath)
				updates[discussionPath] = string(content)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to walk directory: %w", err)
		}
	} else {
		// Attach single file
		content, err := os.ReadFile(attachPath)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		discussionPath := filepath.Join("discussion/files", baseName)
		updates[discussionPath] = string(content)
	}

	// Update meta.tony timestamp
	metaCmd := exec.Command("git", "show", ref+":meta.tony")
	metaOut, err := metaCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to read meta.tony: %w", err)
	}

	metaNode, err := parse.Parse(metaOut)
	if err != nil {
		return fmt.Errorf("failed to parse meta.tony: %w", err)
	}

	issue, err := NodeToMeta(metaNode)
	if err != nil {
		return fmt.Errorf("failed to parse issue metadata: %w", err)
	}

	issue.Updated = time.Now()

	newMetaNode := issue.MetaToNode()
	newMetaContent := encode.MustString(newMetaNode)
	updates["meta.tony"] = newMetaContent

	// Create commit message
	fileCount := len(updates) - 1 // Subtract meta.tony
	commitMsg := fmt.Sprintf("attach: %s (%d file(s))", baseName, fileCount)

	if err := repo.UpdateIssueCommitByRef(ref, commitMsg, updates); err != nil {
		return fmt.Errorf("failed to attach files: %w", err)
	}

	fmt.Printf("Attached %s (%d file(s)) to issue #%06d\n", baseName, fileCount, id)

	return nil
}
