package server

import (
	"fmt"
	"strings"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
)

// MountEntry represents a registered controller mount.
type MountEntry struct {
	Path       string        // The mounted path
	Controller string        // Controller identifier
	Schema     *ir.Node      // Schema for this path
	Session    *MountSession // The session that owns this mount
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

// Lookup returns the mount entry for an exact path, or nil if not mounted.
func (r *MountRegistry) Lookup(path string) *MountEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mounts[path]
}

// LookupPrefix returns the mount entry that owns opPath — the registered mount
// whose path is a segment-prefix of opPath — choosing the longest (most
// specific) match when several apply. Returns nil when opPath is not under any
// mount (a base path served directly from logd).
func (r *MountRegistry) LookupPrefix(opPath string) *MountEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best *MountEntry
	bestLen := -1
	for _, entry := range r.mounts {
		mp := splitPathSegments(entry.Path)
		if !hasSegmentPrefix(splitPathSegments(opPath), mp) {
			continue
		}
		if len(mp) > bestLen {
			best = entry
			bestLen = len(mp)
		}
	}
	return best
}

// splitPathSegments normalizes a mount or operation path into its segments,
// tolerating leading/trailing slashes (mount paths carry a leading "/", client
// op paths generally do not).
func splitPathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// hasSegmentPrefix reports whether prefix is a segment-wise prefix of path. An
// empty prefix (a root mount) matches everything.
func hasSegmentPrefix(path, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
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
