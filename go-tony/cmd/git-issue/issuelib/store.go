package issuelib

import "io"

// Store defines the interface for issue storage operations.
type Store interface {
	// GetNextID allocates and returns the next issue ID.
	GetNextID() (int64, error)

	// Create creates a new issue with the given title and description.
	// Returns the created issue with ID populated.
	Create(title, description string) (*Issue, error)

	// Get retrieves an issue by ID, searching both open and closed refs.
	// Returns the issue, description content, and any error.
	Get(id int64) (*Issue, string, error)

	// GetByRef retrieves an issue by its git ref.
	// Returns the issue, description content, and any error.
	GetByRef(ref string) (*Issue, string, error)

	// FindRef finds the ref for an issue ID, checking both open and closed.
	// Returns the ref path or error if not found.
	FindRef(id int64) (string, error)

	// Update updates an issue's metadata and optionally additional files.
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

	// Push pushes refs to a remote.
	Push(remote string, refspecs []string) error

	// Fetch fetches refs from a remote.
	Fetch(remote string, refspecs []string) error

	// VerifyRemote checks if a remote exists.
	VerifyRemote(remote string) error

	// Out returns the output writer for this store.
	Out() io.Writer
}
