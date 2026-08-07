package issuelib

import "io"

// Store is everything git-issue does to a repository. Commands are written
// against it rather than against git directly, so a test can hand them a store
// aimed at a scratch repository; GitStore is the only implementation.
//
// Methods naming an issue take a full XIDR or any unambiguous prefix of one and
// search open and closed refs alike, so an ID keeps resolving after the issue
// closes. Methods naming a ref take the exact ref and do not search.
//
// Mutations append a commit to the issue's chain and never rewrite it, so an
// issue's history survives every edit.
type Store interface {
	// NewXIDR generates and returns a new XIDR for a new issue.
	NewXIDR() string

	// Create creates a new issue with the given title and description.
	// Returns the created issue with ID populated.
	Create(title, description string) (*Issue, error)

	// Get retrieves an issue by XIDR or XIDR prefix, searching both open and closed refs.
	// Returns the issue, description content, and any error.
	Get(xidrOrPrefix string) (*Issue, string, error)

	// GetByRef retrieves an issue by its git ref.
	// Returns the issue, description content, and any error.
	GetByRef(ref string) (*Issue, string, error)

	// FindRef finds the ref for an issue by XIDR or XIDR prefix, checking both open and closed.
	// Returns the ref path or error if not found or ambiguous.
	FindRef(xidrOrPrefix string) (string, error)

	// Update commits the issue's metadata forward, along with any extraFiles
	// keyed by path in the issue tree; paths not named keep their content.
	// The issue.Ref must be set. The message is used for the commit.
	Update(issue *Issue, message string, extraFiles map[string]string) error

	// List returns all issues, optionally including closed ones.
	List(includeAll bool) ([]*Issue, error)

	// ListRefs returns all issue refs, optionally including closed ones.
	ListRefs(includeAll bool) ([]string, error)

	// MoveRef moves an issue from one ref to another (e.g., close issue).
	MoveRef(from, to string) error

	// ReadFile reads a file from an issue's tree.
	ReadFile(ref, path string) ([]byte, error)

	// ListDir lists directory contents at a path within a ref.
	// Returns a map of name -> "type:hash" (e.g., "blob:abc123" or "tree:def456").
	ListDir(ref, path string) (map[string]string, error)

	// GetRefCommit returns the commit SHA for a ref.
	GetRefCommit(ref string) (string, error)

	// GetCommitInfo returns the short commit info for a SHA.
	GetCommitInfo(sha string) (string, error)

	// VerifyCommit verifies a commit exists and returns its full SHA.
	VerifyCommit(commit string) (string, error)

	// AddNote adds a git note to a commit.
	AddNote(commit, content string) error

	// GetNotes returns the git notes for a commit.
	GetNotes(commit string) (string, error)

	// Push pushes refspecs to a remote. A refspec that fails is reported on
	// Out and skipped, so one bad ref does not abandon the rest.
	Push(remote string, refspecs []string) error

	// Fetch fetches refspecs from a remote, skipping failures as Push does.
	Fetch(remote string, refspecs []string) error

	// VerifyRemote checks if a remote exists.
	VerifyRemote(remote string) error

	// CleanupStaleRefs removes duplicate refs when an issue exists in both
	// refs/issues/ and refs/closed/. Keeps the ref with more history.
	CleanupStaleRefs() (int, error)

	// Out returns the output writer for this store.
	Out() io.Writer
}
