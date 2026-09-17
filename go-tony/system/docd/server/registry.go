package server

import (
	"errors"
	"fmt"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
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

// ErrPathOverlapsMount is the refusal of a mount whose path lies above or below a
// mount already registered, live or tombstoned.
var ErrPathOverlapsMount = errors.New("path overlaps a mount")

// MountRegistry tracks controller mount registrations.
// Each path can only be mounted by one controller at a time, and mounts are
// disjoint: no mount lies above or below another.
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
// path or a mount, live or tombstoned, lies above or below it (ErrPathOverlapsMount).
//
// Mounts are disjoint. A mount under another would give two controllers state for
// one path: the outer holds its whole subtree, so every operation at the outer
// mount would be composed with the inner, every write there split, and every
// watch stream trimmed level by level -- each a place to get wrong (hs9fge9r, and
// 05d8w3cj item 13) for a shape nothing needs. Delegation within a subtree is the
// owning controller's to arrange. A tombstone counts: it is a claim awaiting its
// remount, and a mount inside it would refuse that remount.
func (r *MountRegistry) Register(entry *MountEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing := r.mounts[entry.Path]; existing.Live() {
		return fmt.Errorf("path %q already mounted", entry.Path)
	}
	fields, err := pathFields(entry.Path)
	if err != nil {
		return err
	}
	for path, other := range r.mounts {
		if path == entry.Path {
			continue
		}
		of, err := pathFields(path)
		if err != nil {
			continue
		}
		var where string
		switch {
		case hasFieldPrefix(fields, of):
			where = "under"
		case hasFieldPrefix(of, fields):
			where = "above"
		default:
			continue
		}
		state := "live"
		if !other.Live() {
			state = "tombstoned"
		}
		return fmt.Errorf("%w: %q is %s the %s mount %q", ErrPathOverlapsMount, entry.Path, where, state, path)
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
// whose (kpath) path is a field-prefix of opPath. Mounts are disjoint, so at most
// one applies. Returns nil when opPath is not under any mount (a base path served
// directly from logd) or is not a valid path.
func (r *MountRegistry) LookupPrefix(opPath string) *MountEntry {
	// The FIELD PREFIX, not the whole path: an operation may address an array element
	// (a.votes[0]), and a mount path is field-only, so what owns it is decided by the
	// fields alone. Asking pathFields here answered "no mount" for every such path, which
	// routed a write to base that belonged to a controller (yy0cfe9mh12kr6pwgsn0).
	opFields, _, err := fieldPrefix(opPath)
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

// SetReaches answers a mount the set a wildcard pattern names crosses -- one at, under or
// above a member of the set -- or nil when no mount is near it and the set is logd's
// alone. The pattern is compared with each mount path segment by segment: a field names
// the mount's segment or does not, a field or key wildcard may name any (an element of a
// keyed array is stored under a field), a key may name one, and an index names none,
// since a mount path is field-only. A `..` may name any run of them, of at most depth
// segments (kpath.Unbounded for any number). Every segment the two share agreeing is a
// crossing, whichever is longer: a mount deeper than the pattern lies inside a member,
// and a shallower one holds the members.
func (r *MountRegistry) SetReaches(pattern string, depth int) *MountEntry {
	kp, err := kpath.Parse(pattern)
	if err != nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.mounts {
		mf, err := pathFields(entry.Path)
		if err != nil {
			continue
		}
		if patternReaches(kp, mf, depth) {
			return entry
		}
	}
	return nil
}

// patternReaches says whether the pattern's segments agree with the mount's fields over
// the length they share, a `..` taking at most depth of them.
func patternReaches(kp *kpath.KPath, mount []string, depth int) bool {
	x := kp
	for i, field := range mount {
		if x == nil {
			return true // the mount lies inside a member
		}
		switch {
		case x.Descend:
			if depth == kpath.Unbounded {
				return true // any depth: the mount is somewhere in it
			}
			// The descent takes some of the mount's fields, up to its depth, and the
			// rest of the pattern is tried against what is left.
			for k := 0; k <= depth && i+k <= len(mount); k++ {
				if patternReaches(x.Next, mount[i+k:], depth) {
					return true
				}
			}
			return false
		case x.Field != nil:
			if *x.Field != field {
				return false
			}
		case x.FieldAll, x.KeyAll, x.Key != nil:
			// any field, or an element stored under one
		default:
			return false // an index or sparse index names no field
		}
		x = x.Next
	}
	return true // the mount holds the members
}

// MountsUnder returns every mount whose path lies strictly below opPath (opPath
// is a proper field-prefix of the mount path). Tombstones are included so a
// composed read can detect an unavailable subtree rather than silently omit it.
// Returns nil when no mount lies under opPath, which is always so for a path at
// or under a mount.
func (r *MountRegistry) MountsUnder(opPath string) []*MountEntry {
	// As LookupPrefix: the field prefix decides. Nothing can be mounted below an index,
	// so a path holding one has no mounts under it beyond those under its prefix.
	opFields, indexed, err := fieldPrefix(opPath)
	if err != nil {
		return nil
	}
	if indexed {
		// Under an element of an array at opFields, there is no mount: a mount path is
		// field-only. Mounts under the PREFIX are a different question, and asking it
		// here would claim they lie under this path when they lie beside it.
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []*MountEntry
	for _, entry := range r.mounts {
		mf, err := pathFields(entry.Path)
		if err != nil {
			continue
		}
		if len(mf) > len(opFields) && hasFieldPrefix(mf, opFields) {
			out = append(out, entry)
		}
	}
	return out
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
