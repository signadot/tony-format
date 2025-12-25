package issuelib

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// GitStore implements Store using git refs and objects.
type GitStore struct {
	out io.Writer
}

// NewGitStore creates a new GitStore.
func NewGitStore() *GitStore {
	return &GitStore{out: os.Stdout}
}

// NewGitStoreWithOutput creates a GitStore with custom output.
func NewGitStoreWithOutput(out io.Writer) *GitStore {
	return &GitStore{out: out}
}

func (s *GitStore) Out() io.Writer {
	return s.out
}

// GetNextID allocates the next issue ID.
// It uses the higher of: (stored counter + 1) or (max existing ID + 1)
// to ensure IDs are never reused even if the counter is corrupted.
func (s *GitStore) GetNextID() (int64, error) {
	// Read stored counter
	var counterValue int64
	cmd := exec.Command("git", "show", "refs/meta/issue-counter")
	out, err := cmd.Output()
	if err != nil {
		counterValue = 0
	} else {
		counterValue, err = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			counterValue = 0
		}
	}

	// Find max ID from existing refs (both open and closed)
	maxExistingID := s.findMaxExistingID()

	// Next ID is max of (counter, maxExisting) + 1
	nextID := counterValue
	if maxExistingID > nextID {
		nextID = maxExistingID
	}
	nextID++

	// Write new counter value
	hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	hashCmd.Stdin = strings.NewReader(fmt.Sprintf("%d\n", nextID))
	hashOut, err := hashCmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to hash counter: %w", err)
	}
	hash := strings.TrimSpace(string(hashOut))

	updateCmd := exec.Command("git", "update-ref", "refs/meta/issue-counter", hash)
	if err := updateCmd.Run(); err != nil {
		return 0, fmt.Errorf("failed to update counter: %w", err)
	}

	return nextID, nil
}

// findMaxExistingID scans all issue refs to find the highest ID.
func (s *GitStore) findMaxExistingID() int64 {
	var maxID int64

	// Check both open and closed refs
	for _, pattern := range []string{"refs/issues/*", "refs/closed/*"} {
		cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", pattern)
		out, err := cmd.Output()
		if err != nil {
			continue
		}

		refs := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, ref := range refs {
			if ref == "" {
				continue
			}
			id, err := IDFromRef(ref)
			if err != nil {
				continue
			}
			if id > maxID {
				maxID = id
			}
		}
	}

	return maxID
}

// Create creates a new issue.
func (s *GitStore) Create(title, description string) (*Issue, error) {
	id, err := s.GetNextID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	issue := &Issue{
		ID:       id,
		Ref:      RefForID(id),
		Status:   "open",
		Created:  now,
		Updated:  now,
		Title:    title,
		Commits:  []string{},
		Branches: []string{},
	}

	metaNode, err := issue.ToTonyIR()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize issue: %w", err)
	}
	metaContent := encode.MustString(metaNode)

	// Hash meta.tony
	metaCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	metaCmd.Stdin = strings.NewReader(metaContent)
	metaOut, err := metaCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to hash meta.tony: %w", err)
	}
	metaHash := strings.TrimSpace(string(metaOut))

	// Hash description.md
	descCmd := exec.Command("git", "hash-object", "-w", "--stdin")
	descCmd.Stdin = strings.NewReader(description)
	descOut, err := descCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to hash description.md: %w", err)
	}
	descHash := strings.TrimSpace(string(descOut))

	// Create tree
	treeInput := fmt.Sprintf("100644 blob %s\tdescription.md\n100644 blob %s\tmeta.tony\n", descHash, metaHash)
	treeCmd := exec.Command("git", "mktree")
	treeCmd.Stdin = strings.NewReader(treeInput)
	treeOut, err := treeCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to create tree: %w", err)
	}
	treeHash := strings.TrimSpace(string(treeOut))

	// Create commit
	commitMsg := fmt.Sprintf("create: issue %s", FormatID(id))
	commitCmd := exec.Command("git", "commit-tree", treeHash, "-m", commitMsg)
	commitOut, err := commitCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to create commit: %w", err)
	}
	commitHash := strings.TrimSpace(string(commitOut))

	// Update ref
	updateCmd := exec.Command("git", "update-ref", issue.Ref, commitHash)
	if err := updateCmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to create ref: %w", err)
	}

	return issue, nil
}

// Get retrieves an issue by ID.
func (s *GitStore) Get(id int64) (*Issue, string, error) {
	ref, err := s.FindRef(id)
	if err != nil {
		return nil, "", err
	}
	return s.GetByRef(ref)
}

// GetByRef retrieves an issue by ref.
func (s *GitStore) GetByRef(ref string) (*Issue, string, error) {
	// Read meta.tony
	metaCmd := exec.Command("git", "show", ref+":meta.tony")
	metaOut, err := metaCmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read meta.tony: %w", err)
	}

	metaNode, err := parse.Parse(metaOut)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse meta.tony: %w", err)
	}

	issue := &Issue{}
	if err := issue.FromTonyIR(metaNode); err != nil {
		return nil, "", fmt.Errorf("failed to convert meta to issue: %w", err)
	}
	issue.Ref = ref

	// Read description.md
	descCmd := exec.Command("git", "show", ref+":description.md")
	descOut, err := descCmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("failed to read description.md: %w", err)
	}

	desc := string(descOut)
	lines := strings.Split(desc, "\n")
	if len(lines) > 0 {
		issue.Title = strings.TrimPrefix(lines[0], "# ")
	}

	return issue, desc, nil
}

// FindRef finds the ref for an issue ID.
func (s *GitStore) FindRef(id int64) (string, error) {
	// Try open issues first
	ref := RefForID(id)
	checkCmd := exec.Command("git", "show-ref", ref)
	if err := checkCmd.Run(); err == nil {
		return ref, nil
	}

	// Try closed issues
	ref = ClosedRefForID(id)
	checkCmd = exec.Command("git", "show-ref", ref)
	if err := checkCmd.Run(); err == nil {
		return ref, nil
	}

	return "", fmt.Errorf("issue not found: %s", FormatID(id))
}

// Update updates an issue's metadata.
func (s *GitStore) Update(issue *Issue, message string, extraFiles map[string]string) error {
	if issue.Ref == "" {
		return fmt.Errorf("issue ref not set")
	}

	issue.Updated = time.Now()
	metaNode, err := issue.ToTonyIR()
	if err != nil {
		return fmt.Errorf("failed to serialize issue: %w", err)
	}
	metaContent := encode.MustString(metaNode)

	updates := make(map[string]string)
	updates["meta.tony"] = metaContent
	for k, v := range extraFiles {
		updates[k] = v
	}

	return s.updateCommit(issue.Ref, message, updates)
}

// updateCommit adds a new commit to an issue chain.
func (s *GitStore) updateCommit(ref, message string, updates map[string]string) error {
	// Get current commit
	showCmd := exec.Command("git", "show-ref", ref)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("ref not found: %s", ref)
	}
	currentCommit := strings.Fields(string(showOut))[0]

	// Use a temporary index
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
		hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
		hashCmd.Stdin = strings.NewReader(content)
		hashOut, err := hashCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", path, err)
		}
		hash := strings.TrimSpace(string(hashOut))

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

// List returns all issues.
func (s *GitStore) List(includeAll bool) ([]*Issue, error) {
	refs, err := s.ListRefs(includeAll)
	if err != nil {
		return nil, err
	}

	var issues []*Issue
	for _, ref := range refs {
		issue, _, err := s.GetByRef(ref)
		if err != nil {
			continue // Skip issues that can't be read
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

// ListRefs returns all issue refs.
func (s *GitStore) ListRefs(includeAll bool) ([]string, error) {
	patterns := []string{"refs/issues/*"}
	if includeAll {
		patterns = append(patterns, "refs/closed/*")
	}

	var allRefs []string
	for _, pattern := range patterns {
		cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", pattern)
		out, err := cmd.Output()
		if err != nil {
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

// MoveRef moves an issue from one ref to another.
func (s *GitStore) MoveRef(from, to string) error {
	// Get current commit SHA
	showCmd := exec.Command("git", "show-ref", from)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get current commit: %w", err)
	}
	commitSHA := strings.Fields(string(showOut))[0]

	// Create new ref
	updateCmd := exec.Command("git", "update-ref", to, commitSHA)
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to create new ref: %w", err)
	}

	// Delete old ref
	deleteCmd := exec.Command("git", "update-ref", "-d", from)
	if err := deleteCmd.Run(); err != nil {
		return fmt.Errorf("failed to delete old ref: %w", err)
	}

	return nil
}

// ReadFile reads a file from an issue's tree.
func (s *GitStore) ReadFile(ref, path string) ([]byte, error) {
	cmd := exec.Command("git", "show", ref+":"+path)
	return cmd.Output()
}

// GetRefCommit returns the commit SHA for a ref.
func (s *GitStore) GetRefCommit(ref string) (string, error) {
	cmd := exec.Command("git", "show-ref", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ref not found: %s", ref)
	}
	return strings.Fields(string(out))[0], nil
}

// GetCommitInfo returns the short commit info.
func (s *GitStore) GetCommitInfo(sha string) (string, error) {
	cmd := exec.Command("git", "log", "-1", "--oneline", sha)
	out, err := cmd.Output()
	if err != nil {
		return sha[:7], nil
	}
	return strings.TrimSpace(string(out)), nil
}

// VerifyCommit verifies a commit exists.
func (s *GitStore) VerifyCommit(commit string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", commit)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("commit not found: %s", commit)
	}
	return strings.TrimSpace(string(out)), nil
}

// AddNote adds a git note to a commit.
func (s *GitStore) AddNote(commit, content string) error {
	// Check if note exists
	checkCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "show", commit)
	checkOut, checkErr := checkCmd.Output()

	if checkErr == nil {
		// Note exists, check if already contains this content
		existingLines := strings.Split(strings.TrimSpace(string(checkOut)), "\n")
		for _, line := range existingLines {
			if strings.TrimSpace(line) == content {
				return nil // Already exists
			}
		}
		// Append to existing note
		appendCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "append", "-m", content, commit)
		return appendCmd.Run()
	}

	// Create new note
	addCmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "add", "-m", content, commit)
	return addCmd.Run()
}

// GetNotes returns the git notes for a commit.
func (s *GitStore) GetNotes(commit string) (string, error) {
	cmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "show", commit)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Push pushes refs to a remote.
func (s *GitStore) Push(remote string, refspecs []string) error {
	for _, refspec := range refspecs {
		cmd := exec.Command("git", "push", remote, refspec)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if !strings.Contains(string(output), "does not match any") {
				fmt.Fprintf(s.out, "Warning: failed to push %s: %s\n", refspec, string(output))
			}
		}
	}
	return nil
}

// Fetch fetches refs from a remote.
func (s *GitStore) Fetch(remote string, refspecs []string) error {
	for _, refspec := range refspecs {
		cmd := exec.Command("git", "fetch", remote, refspec)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if !strings.Contains(string(output), "couldn't find remote ref") {
				fmt.Fprintf(s.out, "Warning: failed to fetch %s: %s\n", refspec, string(output))
			}
		}
	}
	return nil
}

// VerifyRemote checks if a remote exists.
func (s *GitStore) VerifyRemote(remote string) error {
	cmd := exec.Command("git", "remote", "get-url", remote)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remote not found: %s", remote)
	}
	return nil
}

// GetTree reads the tree for a ref and returns entries.
func (s *GitStore) GetTree(ref string) (map[string]string, error) {
	return s.ListDir(ref, "")
}

// ReplaceTree replaces the entire tree with new files and creates a commit.
func (s *GitStore) ReplaceTree(ref, message string, files map[string][]byte) error {
	// Get current commit as parent
	showCmd := exec.Command("git", "show-ref", ref)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("ref not found: %s", ref)
	}
	currentCommit := strings.Fields(string(showOut))[0]

	// Use a temporary index
	tmpIndex := fmt.Sprintf("/tmp/git-issue-index-%d", time.Now().UnixNano())
	defer os.Remove(tmpIndex)

	// Hash all files and build index
	for path, content := range files {
		// Hash the content
		hashCmd := exec.Command("git", "hash-object", "-w", "--stdin")
		hashCmd.Stdin = bytes.NewReader(content)
		hashOut, err := hashCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to hash %s: %w", path, err)
		}
		hash := strings.TrimSpace(string(hashOut))

		// Add to index
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

// CleanupStaleRefs removes stale refs when an issue exists in both refs/issues/ and refs/closed/.
// For each duplicate, it keeps the ref with more history (the descendant) and deletes the ancestor.
// Returns the number of refs cleaned up.
func (s *GitStore) CleanupStaleRefs() (int, error) {
	// Get all open issue IDs
	openCmd := exec.Command("git", "for-each-ref", "--format=%(refname)", "refs/issues/*")
	openOut, _ := openCmd.Output()
	openRefs := strings.Split(strings.TrimSpace(string(openOut)), "\n")

	cleaned := 0
	for _, openRef := range openRefs {
		if openRef == "" {
			continue
		}
		id, err := IDFromRef(openRef)
		if err != nil {
			continue
		}

		// Check if closed ref also exists
		closedRef := ClosedRefForID(id)
		checkCmd := exec.Command("git", "show-ref", closedRef)
		if checkCmd.Run() != nil {
			continue // No duplicate
		}

		// Both refs exist - determine which to keep based on ancestry
		// If open is ancestor of closed, delete open (closed has more history)
		// If closed is ancestor of open, delete closed (open has more history)
		// If neither is ancestor, keep closed (it's explicitly marked closed)

		openSHA, _ := s.GetRefCommit(openRef)
		closedSHA, _ := s.GetRefCommit(closedRef)

		var refToDelete string
		if s.isAncestor(openSHA, closedSHA) {
			// open is ancestor of closed - closed has more history, delete open
			refToDelete = openRef
		} else if s.isAncestor(closedSHA, openSHA) {
			// closed is ancestor of open - open has more history, delete closed
			refToDelete = closedRef
		} else {
			// No ancestry relationship - keep closed (explicit close wins)
			refToDelete = openRef
		}

		deleteCmd := exec.Command("git", "update-ref", "-d", refToDelete)
		if err := deleteCmd.Run(); err != nil {
			fmt.Fprintf(s.out, "Warning: failed to delete stale ref %s: %v\n", refToDelete, err)
			continue
		}
		cleaned++
	}

	return cleaned, nil
}

// isAncestor returns true if ancestor is an ancestor of descendant.
func (s *GitStore) isAncestor(ancestor, descendant string) bool {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant)
	return cmd.Run() == nil
}

// ListDir lists directory contents at a path within a ref.
func (s *GitStore) ListDir(ref, path string) (map[string]string, error) {
	target := ref
	if path != "" {
		target = ref + ":" + path
	} else {
		target = ref + "^{tree}"
	}
	cmd := exec.Command("git", "cat-file", "-p", target)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 4 {
			typ := parts[1]
			hash := parts[2]
			name := strings.Join(parts[3:], " ")
			result[name] = typ + ":" + hash
		}
	}
	return result, nil
}
