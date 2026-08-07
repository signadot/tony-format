package issuelib

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// GitStore implements Store on git refs and objects.
//
// It works by driving the git binary -- plumbing commands (hash-object, mktree,
// commit-tree, update-ref) over a temporary index, so writing an issue never
// touches the caller's index or working tree, and a repository with uncommitted
// work is a fine place to file one. Nothing is cached: each call asks git.
//
// The repository is the git binary's own choice of one, meaning the process's
// working directory. A GitStore holds no path, so a test selects its repository
// with t.Chdir and callers must not run two against different repositories
// concurrently.
type GitStore struct {
	out io.Writer
}

// NewGitStore creates a GitStore that reports warnings on stdout.
func NewGitStore() *GitStore {
	return &GitStore{out: os.Stdout}
}

// NewGitStoreWithOutput creates a GitStore writing its warnings to out.
func NewGitStoreWithOutput(out io.Writer) *GitStore {
	return &GitStore{out: out}
}

// Out returns the writer the store reports warnings on.
func (s *GitStore) Out() io.Writer {
	return s.out
}

// NewXIDR mints an XIDR for the current time. It allocates nothing in the
// repository -- uniqueness comes from the XID itself, not from a counter ref
// that two clones would have to agree on.
func (s *GitStore) NewXIDR() string {
	xid := NewXID(time.Now())
	return xid.XIDR()
}

// Create writes a new open issue: a root commit holding description.md and
// meta.tony, and refs/issues/<xidr> pointing at it. The returned Issue has its
// ID and Ref filled in.
func (s *GitStore) Create(title, description string) (*Issue, error) {
	xidr := s.NewXIDR()

	now := time.Now()
	issue := &Issue{
		ID:       xidr,
		Ref:      RefForXIDR(xidr),
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
	commitMsg := fmt.Sprintf("create: issue %s", xidr)
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

// Get retrieves an issue by XIDR or XIDR prefix, open or closed, and returns it
// with the text of its description.
func (s *GitStore) Get(xidOrPrefix string) (*Issue, string, error) {
	ref, err := s.FindRef(xidOrPrefix)
	if err != nil {
		return nil, "", err
	}
	return s.GetByRef(ref)
}

// GetByRef reads the issue at ref and the text of its description. Ref and
// Title are derived here rather than read from meta.tony: Ref is where the issue
// was found, Title the first line of description.md with any "# " stripped.
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

// FindRef finds the ref for an issue by XIDR or XIDR prefix.
// Returns error if not found or if prefix matches multiple issues.
func (s *GitStore) FindRef(xidrOrPrefix string) (string, error) {
	// If it's a full 20-char XIDR, try exact match first
	if len(xidrOrPrefix) == 20 {
		ref := RefForXIDR(xidrOrPrefix)
		checkCmd := exec.Command("git", "show-ref", ref)
		if err := checkCmd.Run(); err == nil {
			return ref, nil
		}

		ref = ClosedRefForXIDR(xidrOrPrefix)
		checkCmd = exec.Command("git", "show-ref", ref)
		if err := checkCmd.Run(); err == nil {
			return ref, nil
		}

		return "", fmt.Errorf("issue not found: %s", xidrOrPrefix)
	}

	// Prefix search - find all matching refs
	var matches []string
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
			xidr, err := XIDRFromRef(ref)
			if err != nil {
				continue
			}
			if MatchesXIDRPrefix(xidrOrPrefix, xidr) {
				matches = append(matches, ref)
			}
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("issue not found: %s", xidrOrPrefix)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous prefix %q matches %d issues", xidrOrPrefix, len(matches))
	}

	return matches[0], nil
}

// Update commits issue.Ref forward with a rewritten meta.tony and any
// extraFiles, each keyed by its path in the issue tree. Paths not mentioned keep
// the content they had, and issue.Updated is stamped as a side effect. The
// issue's Ref must be set, which it is for anything that came out of Get,
// GetByRef, List or Create.
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

// updateCommit adds a commit to an issue chain, carrying the previous tree
// forward through a temporary index and overwriting only the given paths.
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

// List returns the open issues, or every issue when includeAll is set. An issue
// whose tree cannot be read is skipped rather than failing the listing, so one
// damaged ref does not hide the rest.
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

// ListRefs returns the refs under refs/issues/, plus refs/closed/ when
// includeAll is set.
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

// MoveRef repoints to at from's commit and deletes from. This is how an issue
// changes status: the commit chain is untouched, only the namespace changes.
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

// ReadFile returns the bytes of path within ref's tree, or an error if there is
// no such file.
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

// GetCommitInfo returns the commit's "git log --oneline" line. A commit that is
// not in this repository -- an issue can outlive the branch its commits were on
// -- degrades to the abbreviated SHA rather than an error, so listings still
// have something to print.
func (s *GitStore) GetCommitInfo(sha string) (string, error) {
	cmd := exec.Command("git", "log", "-1", "--oneline", sha)
	out, err := cmd.Output()
	if err != nil {
		return sha[:7], nil
	}
	return strings.TrimSpace(string(out)), nil
}

// VerifyCommit resolves any commit-ish -- "HEAD", a branch, an abbreviated SHA
// -- to a full SHA, erroring if it names nothing. Commits are recorded on issues
// in resolved form so the reference stays meaningful after the branch moves.
func (s *GitStore) VerifyCommit(commit string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--verify", commit)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("commit not found: %s", commit)
	}
	return strings.TrimSpace(string(out)), nil
}

// AddNote records content in the commit's note under refs/notes/issues, which
// is the reverse index that answers "which issues mention this commit". It
// appends to an existing note and is idempotent: content already present as a
// line is not added twice.
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

// GetNotes returns the commit's refs/notes/issues note, one issue ID per line.
// A commit with no note is an error, not an empty string.
func (s *GitStore) GetNotes(commit string) (string, error) {
	cmd := exec.Command("git", "notes", "--ref=refs/notes/issues", "show", commit)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Push pushes each refspec to the remote in turn. A refspec that fails is
// reported on Out and skipped rather than returned as an error, so one
// unpushable issue does not abandon the rest; a refspec matching nothing locally
// is not worth mentioning and stays quiet.
//
// Callers pass force refspecs ("+src:dst"). Issue refs are rewritten in place by
// migrations and moved between namespaces on close, so a non-force push would
// reject exactly the updates that need to travel. The cost is that the last
// writer of an issue wins; see the commands package.
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

// Fetch fetches each refspec from the remote, warning on Out and continuing
// when one fails, as Push does. A refspec the remote does not have is silently
// skipped: a repository with no closed issues yet is not an error.
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

// GetTree lists the top level of an issue's tree, in ListDir's format.
func (s *GitStore) GetTree(ref string) (map[string]string, error) {
	return s.ListDir(ref, "")
}

// ReplaceTree commits ref forward with a tree built from files alone. Unlike
// Update, nothing is carried over: a path absent from files is gone from the new
// tree, which is what a rewrite such as migration wants and what an ordinary
// edit does not. The old tree stays reachable through the commit's parent.
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
	// Get all open issue XIDs
	openCmd := exec.Command("git", "for-each-ref", "--format=%(refname)", "refs/issues/*")
	openOut, _ := openCmd.Output()
	openRefs := strings.Split(strings.TrimSpace(string(openOut)), "\n")

	cleaned := 0
	for _, openRef := range openRefs {
		if openRef == "" {
			continue
		}
		xidr, err := XIDRFromRef(openRef)
		if err != nil {
			continue
		}

		// Check if closed ref also exists
		closedRef := ClosedRefForXIDR(xidr)
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

// ListDir lists one level of an issue tree: the entries directly under path
// within ref, or the tree root when path is empty. Each entry maps its name to
// "type:hash", e.g. "blob:abc123" or "tree:def456", so a caller can tell a file
// from a subdirectory without a second lookup.
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
