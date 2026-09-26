package issuelib

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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
	// warn is where a read says it corrected an issue's shape: stderr, so it
	// never mixes into what a command prints.
	warn io.Writer
	// adopted guards the implicit adoption of refs an older git-issue wrote,
	// which every read of an existing issue goes through (adoptOnce).
	adopted sync.Once
}

// NewGitStore creates a GitStore that reports warnings on stdout, and what a
// read corrected on stderr.
func NewGitStore() *GitStore {
	return &GitStore{out: os.Stdout, warn: os.Stderr}
}

// NewGitStoreWithOutput creates a GitStore writing both to out.
func NewGitStoreWithOutput(out io.Writer) *GitStore {
	return &GitStore{out: out, warn: out}
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
	if err := s.setRef(issue.Ref, commitHash, zeroSHA); err != nil {
		return nil, fmt.Errorf("failed to create ref: %w", err)
	}

	return issue, nil
}

// zeroSHA is the old value that tells update-ref the ref must not exist yet.
const zeroSHA = "0000000000000000000000000000000000000000"

// errRefMoved is answered by setRef when the ref is not at the value the caller
// read: another writer got there first, and the caller's change was built on a
// commit that is no longer the tip.
var errRefMoved = errors.New("ref moved")

// setRef points ref at commit, provided it is at old now (zeroSHA: provided it
// does not exist). A write of the ref that did not say what it expected the ref
// to hold overwrote whatever another writer had put there between this writer's
// read and its write: eight `git issue comment` runs at once all said "Added
// comment" and one comment survived (05d8w3cjh12kswb1msn0).
//
// --create-reflog because git logs only refs/heads/, refs/remotes/, refs/notes/
// and HEAD by default, whatever core.logAllRefUpdates says, and an issue ref is
// none of those: what a forced sync overwrote was a dangling commit with nothing
// recording that it had been there. Git goes on logging any ref whose log
// exists, so one write through here is enough to cover every later overwrite,
// a fetch's included. The setting itself is the user's and covers refs that are
// not ours, so it is not touched.
func (s *GitStore) setRef(ref, commit, old string) error {
	cmd := exec.Command("git", "update-ref", "--create-reflog", ref, commit, old)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "cannot lock ref") {
			return fmt.Errorf("%w: %s", errRefMoved, strings.TrimSpace(stderr.String()))
		}
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// tempIndexSeq numbers the temporary indexes this process has minted.
var tempIndexSeq atomic.Uint64

// tempIndexPath names a temporary index no other writer is using. A name minted
// from the clock alone was shared by two writers starting in the same instant,
// and one's write-tree read the other's index.
func tempIndexPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("git-issue-index-%d-%d-%d",
		os.Getpid(), time.Now().UnixNano(), tempIndexSeq.Add(1)))
}

// retryRefMoved runs a read-modify-write of a ref until it lands on the tip it
// read, or fails for another reason.
func retryRefMoved(attempt func() error) error {
	var err error
	for i := 0; i < 32; i++ {
		if err = attempt(); !errors.Is(err, errRefMoved) {
			return err
		}
		time.Sleep(time.Duration(rand.Intn(20)+1) * time.Millisecond)
	}
	return err
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
	s.singleValueLabels(issue)

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

// singleValueLabels leaves each of the issue's label keys holding one value,
// the last its list gives, and warns for every key that held more. Only a merge
// made by an older git-issue writes that shape, and which value was meant is
// for a person to say.
func (s *GitStore) singleValueLabels(issue *Issue) {
	labels, multi := singleValued(issue.Labels)
	if multi == nil {
		return
	}
	issue.Labels = labels
	for _, k := range slices.Sorted(maps.Keys(multi)) {
		vs := multi[k]
		fmt.Fprintf(s.warn, "warning: issue %s: label key %s has %d values (%s); reading %s. "+
			"Fix with `git issue label %s %s=<value>`.\n",
			FormatID(issue.ID), k, len(vs), strings.Join(vs, ", "), vs[len(vs)-1], FormatID(issue.ID), k)
	}
}

// FindRef finds the ref for an issue by XIDR or XIDR prefix.
// Returns error if not found or if prefix matches multiple issues.
func (s *GitStore) FindRef(xidrOrPrefix string) (string, error) {
	s.adoptOnce()

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
	for _, r := range refsAt(OpenPrefix+"*", ClosedPrefix+"*") {
		xidr, err := XIDRFromRef(r.ref)
		if err != nil {
			continue
		}
		if MatchesXIDRPrefix(xidrOrPrefix, xidr) {
			matches = append(matches, r.ref)
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
	return retryRefMoved(func() error { return s.updateCommitOnce(ref, message, updates) })
}

// updateCommitOnce is one attempt of updateCommit: it answers errRefMoved when
// the ref moved between its read and its write, and the updates are applied
// again on the new tip. A path written here carries the caller's whole view of
// it, so two writers changing meta.tony at once still see the last one win on
// that file; what no longer happens is one writer's commit, files and all,
// vanishing under the other's.
func (s *GitStore) updateCommitOnce(ref, message string, updates map[string]string) error {
	// Get current commit
	showCmd := exec.Command("git", "show-ref", ref)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("ref not found: %s", ref)
	}
	currentCommit := strings.Fields(string(showOut))[0]

	// Use a temporary index
	tmpIndex := tempIndexPath()
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
	if err := s.setRef(ref, commitHash, currentCommit); err != nil {
		if errors.Is(err, errRefMoved) {
			return err
		}
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

// ListRefs returns the open issue refs, plus the closed ones when includeAll is
// set. Anything an older git-issue left in this clone is adopted first, so a
// listing shows every issue whichever binary wrote it (AdoptGen0).
func (s *GitStore) ListRefs(includeAll bool) ([]string, error) {
	s.adoptOnce()

	patterns := []string{OpenPrefix + "*"}
	if includeAll {
		patterns = append(patterns, ClosedPrefix+"*")
	}

	var allRefs []string
	for _, r := range refsAt(patterns...) {
		allRefs = append(allRefs, r.ref)
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
	if err := s.setRef(to, commitSHA, zeroSHA); err != nil {
		return fmt.Errorf("failed to create new ref: %w", err)
	}

	// Delete old ref, provided it is still what was moved
	deleteCmd := exec.Command("git", "update-ref", "-d", from, commitSHA)
	var stderr bytes.Buffer
	deleteCmd.Stderr = &stderr
	if err := deleteCmd.Run(); err != nil {
		return fmt.Errorf("failed to delete old ref: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// ReadFile returns the bytes of path within ref's tree, or an error if there is
// no such path. A path naming a directory is not an error: it returns git's
// listing of that tree, so a caller that needs a file checks the entry's type
// with ListDir first.
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

// AddNote records content in the commit's note under NotesRef, which
// is the reverse index that answers "which issues mention this commit". It
// appends to an existing note and is idempotent: content already present as a
// line is not added twice.
func (s *GitStore) AddNote(commit, content string) error {
	// Check if note exists
	checkCmd := exec.Command("git", "notes", "--ref="+NotesRef, "show", commit)
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
		appendCmd := exec.Command("git", "notes", "--ref="+NotesRef, "append", "-m", content, commit)
		return appendCmd.Run()
	}

	// Create new note
	addCmd := exec.Command("git", "notes", "--ref="+NotesRef, "add", "-m", content, commit)
	return addCmd.Run()
}

// GetNotes returns the commit's NotesRef note, one issue ID per line
// with a blank line between entries, since git notes append separates what it
// adds that way. A commit with no note is an error, not an empty string.
func (s *GitStore) GetNotes(commit string) (string, error) {
	cmd := exec.Command("git", "notes", "--ref="+NotesRef, "show", commit)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// quietSyncFailure says git's complaint is not a failure of the sync: there was
// nothing to do. A refspec matching nothing locally has nothing to send, and a
// ref the remote does not have is already in the state a fetch or a deletion
// wanted -- a repository with no closed issues yet, or a deletion of a ref that
// is already gone.
//
// "No refs in common" is that same nothing, said differently: a glob refspec is
// expanded against the remote's refs, so git connects before finding it matches
// nothing, and a remote with no refs at all leaves it with no refspec to send.
// A concrete refspec resolves locally first and says "does not match any"
// instead, which is why one empty push can be reported either way.
func quietSyncFailure(output string) bool {
	for _, quiet := range []string{
		"does not match any",
		"remote ref does not exist",
		"couldn't find remote ref",
		"No refs in common",
	} {
		if strings.Contains(output, quiet) {
			return true
		}
	}
	return false
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
	return retryRefMoved(func() error { return s.replaceTreeOnce(ref, message, files) })
}

func (s *GitStore) replaceTreeOnce(ref, message string, files map[string][]byte) error {
	// Get current commit as parent
	showCmd := exec.Command("git", "show-ref", ref)
	showOut, err := showCmd.Output()
	if err != nil {
		return fmt.Errorf("ref not found: %s", ref)
	}
	currentCommit := strings.Fields(string(showOut))[0]

	// Use a temporary index
	tmpIndex := tempIndexPath()
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
	if err := s.setRef(ref, commitHash, currentCommit); err != nil {
		if errors.Is(err, errRefMoved) {
			return err
		}
		return fmt.Errorf("failed to update ref: %w", err)
	}

	return nil
}

// CleanupStaleRefs removes stale refs when an issue exists in both the open and closed namespaces.
// For each duplicate, it keeps the ref with more history (the descendant) and deletes the ancestor;
// when neither descends from the other it keeps the closed ref and deletes the open one.
// Returns the number of refs cleaned up.
func (s *GitStore) CleanupStaleRefs() (int, error) {
	cleaned := 0
	for _, open := range refsAt(OpenPrefix + "*") {
		openRef := open.ref
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
