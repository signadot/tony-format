package server

import (
	"fmt"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
)

// MountEntry represents a mount registration. A live mount has a non-nil
// Session. When the owning controller disconnects, the entry is kept as a
// tombstone (Session nil) so operations on the subtree fail with a clear
// "controller unavailable" error instead of silently falling through to logd —
// the mounted content lived in the controller, not logd. A remount clears the
// tombstone.
type MountEntry struct {
	Path       string        // The mounted path
	Controller string        // Controller identifier
	Schema     *ir.Node      // Schema for this path
	Session    *MountSession // The session that owns this mount; nil = tombstone
}

// Live reports whether the entry has a live controller session.
func (e *MountEntry) Live() bool {
	return e != nil && e.Session != nil
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

// Register adds a mount. It succeeds if the path is free or holds a tombstone
// (a crashed controller remounting), and fails if a live mount already owns the
// path.
func (r *MountRegistry) Register(entry *MountEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing := r.mounts[entry.Path]; existing.Live() {
		return fmt.Errorf("path %q already mounted", entry.Path)
	}

	r.mounts[entry.Path] = entry
	return nil
}

// Unregister fully removes a mount by path (used to roll back a failed
// registration). To mark a crashed controller, use TombstoneBySession.
func (r *MountRegistry) Unregister(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.mounts, path)
}

// TombstoneBySession marks the mount at path as tombstoned (controller gone) if
// and only if it is still owned by session. The session check avoids clobbering
// a controller that has already remounted the path.
//
// The entry is replaced with a tombstone copy rather than mutated in place:
// LookupPrefix hands out entry pointers that callers read outside the registry
// lock, so entries must stay immutable once published.
func (r *MountRegistry) TombstoneBySession(path string, session *MountSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.mounts[path]
	if entry == nil || entry.Session != session {
		return
	}
	tomb := *entry
	tomb.Session = nil
	r.mounts[path] = &tomb
}

// Lookup returns the mount entry for an exact path, or nil if not mounted.
func (r *MountRegistry) Lookup(path string) *MountEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mounts[path]
}

// LookupPrefix returns the mount entry that owns opPath — the registered mount
// whose (kpath) path is a field-prefix of opPath — choosing the longest (most
// specific) match when several apply. Returns nil when opPath is not under any
// mount (a base path served directly from logd) or is not a valid path.
func (r *MountRegistry) LookupPrefix(opPath string) *MountEntry {
	opFields, err := pathFields(opPath)
	if err != nil {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var best *MountEntry
	bestLen := -1
	for _, entry := range r.mounts {
		mf, err := pathFields(entry.Path)
		if err != nil {
			continue
		}
		if !hasFieldPrefix(opFields, mf) {
			continue
		}
		if len(mf) > bestLen {
			best = entry
			bestLen = len(mf)
		}
	}
	return best
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
