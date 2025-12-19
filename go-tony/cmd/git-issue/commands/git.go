package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
)

// GitRepo provides git operations
type GitRepo struct{}

// GetNextIssueID reads and increments the issue counter
func (r *GitRepo) GetNextIssueID() (int64, error) {
	// Try to read current counter
	cmd := exec.Command("git", "show", "refs/meta/issue-counter")
	out, err := cmd.Output()

	var current int64
	if err != nil {
		// Counter doesn't exist, start at 1
		current = 1
	} else {
		current, err = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid counter value: %w", err)
		}
		current++
	}

	// Write new counter value
	hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	hashCmd.Stdin = strings.NewReader(fmt.Sprintf("%d\n", current))
	hashOut, err := hashCmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to hash counter: %w", err)
	}
	hash := strings.TrimSpace(string(hashOut))

	// Update ref
	updateCmd := exec.Command("git", "update-ref", "refs/meta/issue-counter", hash)
	if err := updateCmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to update counter: %w", err)
	}

	return current, nil
}

// CreateIssueRef creates a new issue ref with initial tree
func (r *GitRepo) CreateIssueRef(id int64, metaContent, descContent string) error {
	// Hash meta.tony
	metaCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	metaCmd.Stdin = strings.NewReader(metaContent)
	metaOut, err := metaCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to hash meta.tony: %w", err)
	}
	metaHash := strings.TrimSpace(string(metaOut))

	// Hash description.md
	descCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	descCmd.Stdin = strings.NewReader(descContent)
	descOut, err := descCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to hash description.md: %w", err)
	}
	descHash := strings.TrimSpace(string(descOut))

	// Create tree
	treeInput := fmt.Sprintf("100644 blob %s\tdescription.md\n100644 blob %s\tmeta.tony\n", descHash, metaHash)
	treeCmd := exec.Command("git", "mktree")
	treeCmd.Stdin = strings.NewReader(treeInput)
	treeOut, err := treeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to create tree: %w", err)
	}
	treeHash := strings.TrimSpace(string(treeOut))

	// Create commit
	commitMsg := fmt.Sprintf("create: issue %06d", id)
	commitCmd := exec.Command("git", "commit-tree", treeHash, "-m", commitMsg)
	commitOut, err := commitCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}
	commitHash := strings.TrimSpace(string(commitOut))

	// Update ref
	ref := fmt.Sprintf("refs/issues/%06d", id)
	updateCmd := exec.Command("git", "update-ref", ref, commitHash)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to create ref: %w", err)
	}

	return nil
}

// ListIssueRefs lists all issue refs (open and optionally closed)
func (r *GitRepo) ListIssueRefs(includeAll bool) ([]string, error) {
	patterns := []string{"refs/issues/*"}
	if includeAll {
		patterns = append(patterns, "refs/closed/*")
	}

	var allRefs []string
	for _, pattern := range patterns {
		cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", pattern)
		out, err := cmd.Output()
		if err != nil {
			// No refs matching pattern is okay
			continue
		}

		refs := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, ref := range refs {
			if ref != "" {
				allRefs = append(allRefs, ref)
			}
		}
	}

	return allRefs, nil
}

// ReadIssue reads an issue's current state
func (r *GitRepo) ReadIssue(ref string) (*Issue, string, error) {
	// Read meta.tony
	metaCmd := exec.Command("git", "show", ref+":meta.tony")
	metaOut, err := metaCmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read meta.tony: %w", err)
	}

	// Parse meta.tony
	metaNode, err := parse.Parse(metaOut)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse meta.tony: %w", err)
	}

	issue, err := NodeToMeta(metaNode)
	if err != nil {
		return nil, "", fmt.Errorf("failed to convert meta to issue: %w", err)
	}

	// Read description.md (first line is title)
	descCmd := exec.Command("git", "show", ref+":description.md")
	descOut, err := descCmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read description.md: %w", err)
	}

	desc := string(descOut)
	lines := strings.Split(desc, "\n")
	if len(lines) > 0 {
		// Strip markdown heading if present
		title := strings.TrimPrefix(lines[0], "# ")
		issue.Title = title
	}

	return issue, desc, nil
}

// UpdateIssueCommit adds a new commit to an issue chain
func (r *GitRepo) UpdateIssueCommit(id int64, message string, updates map[string]string) error {
	ref := fmt.Sprintf("refs/issues/%06d", id)
	return r.UpdateIssueCommitByRef(ref, message, updates)
}

// UpdateIssueCommitByRef adds a new commit to an issue chain using a ref
func (r *GitRepo) UpdateIssueCommitByRef(ref, message string, updates map[string]string) error {
	// Get current commit
	showCmd := exec.Command("git", "show-ref", ref)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("ref not found: %s", ref)
	}
	currentCommit := strings.Fields(string(showOut))[0]

	// Use a temporary index to build the new tree
	// First, read the current tree into a temporary index
	tmpIndex := fmt.Sprintf("/tmp/git-issue-index-%d", time.Now().UnixNano())
	defer os.Remove(tmpIndex)

	// Read current tree into temporary index
	readTreeCmd := exec.Command("git", "read-tree", currentCommit)
	readTreeCmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	if err := readTreeCmd.Run(); err != nil {
		return fmt.Errorf("failed to read tree: %w", err)
	}

	// Update files in the index
	for path, content := range updates {
		// Hash the content
		hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
		hashCmd.Stdin = strings.NewReader(content)
		hashOut, err := hashCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", path, err)
		}
		hash := strings.TrimSpace(string(hashOut))

		// Update index
		updateIndexCmd := exec.Command("git", "update-index", "--add", "--cacheinfo", "100644", hash, path)
		updateIndexCmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
		if err := updateIndexCmd.Run(); err != nil {
			return fmt.Errorf("failed to update index for %s: %w", path, err)
		}
	}

	// Write tree from index
	writeTreeCmd := exec.Command("git", "write-tree")
	writeTreeCmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+tmpIndex)
	treeOut, err := writeTreeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to write tree: %w", err)
	}
	treeHash := strings.TrimSpace(string(treeOut))

	// Create commit with parent
	commitCmd := exec.Command("git", "commit-tree", treeHash, "-p", currentCommit, "-m", message)
	commitOut, err := commitCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}
	commitHash := strings.TrimSpace(string(commitOut))

	// Update ref
	updateCmd := exec.Command("git", "update-ref", ref, commitHash)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to update ref: %w", err)
	}

	return nil
}
