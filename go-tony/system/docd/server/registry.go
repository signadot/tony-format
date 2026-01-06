package server

import (
	"fmt"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
)

// MountEntry represents a registered controller mount.
type MountEntry struct {
	Path       string           // The mounted path
	Controller string           // Controller identifier
	Schema     *ir.Node         // Schema for this path
	Session    *MountSession    // The session that owns this mount
}

// MountRegistry tracks controller mount registrations.
// Each path can only be mounted by one controller at a time.
type MountRegistry struct {
	mu     sync.RWMutex
	mounts map[string]*MountEntry // path → entry
}

// NewMountRegistry creates a new mount registry.
func NewMountRegistry() *MountRegistry {
	return &MountRegistry{
		mounts: make(map[string]*MountEntry),
	}
}

// Register adds a new mount. Returns error if path is already mounted.
func (r *MountRegistry) Register(entry *MountEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.mounts[entry.Path]; exists {
		return fmt.Errorf("path %q already mounted", entry.Path)
	}

	r.mounts[entry.Path] = entry
	return nil
}

// Unregister removes a mount by path.
func (r *MountRegistry) Unregister(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mounts, path)
}

// UnregisterBySession removes all mounts for a given session.
func (r *MountRegistry) UnregisterBySession(session *MountSession) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for path, entry := range r.mounts {
		if entry.Session == session {
			delete(r.mounts, path)
		}
	}
}

// Lookup returns the mount entry for a path, or nil if not mounted.
func (r *MountRegistry) Lookup(path string) *MountEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mounts[path]
}

// List returns all current mounts.
func (r *MountRegistry) List() []*MountEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]*MountEntry, 0, len(r.mounts))
	for _, entry := range r.mounts {
		entries = append(entries, entry)
	}
	return entries
}
