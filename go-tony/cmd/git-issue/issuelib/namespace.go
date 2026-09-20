package issuelib

// The ref layout, and the generation in it.
//
// An issue is a ref, and which namespace it is in says its status. The
// generation -- the "v1" in the middle -- says which layout a client is looking
// at, and exists for one reason: a client names its refspecs literally, so it
// cannot see or touch a ref it does not name. A client from before the
// generation existed pushes and fetches refs/issues/ and refs/closed/, which is
// gen0 below, and is therefore harmless to anything here.
//
// The generation changes only for a break a client of the previous one cannot
// safely coexist with: a ref layout, or how sync decides what to write. What
// meta.tony holds is not such a break -- a field added there is read by a client
// that does not know it, and moving every ref in every clone for one would be a
// flag day for nothing.
//
// gen0 is the layout before this one, never "legacy": IsLegacyRef already means
// a six-digit numeric id, which is a different thing and older still.
const Generation = "v1"

const (
	// nsRoot is everything this generation owns, tracking refs included.
	nsRoot = "refs/git-issues/" + Generation

	// OpenPrefix and ClosedPrefix hold an issue's ref, keyed by XIDR.
	OpenPrefix   = nsRoot + "/open/"
	ClosedPrefix = nsRoot + "/closed/"

	// NotesRef is the reverse index, commit -> issue ids. Notes refs live under
	// refs/notes/ because that is where git logs them without being asked and
	// where git log --notes= looks for them. It is a ref and not a namespace, so
	// nothing may live beneath it -- which is why the tracking copies below sit
	// elsewhere.
	NotesRef = "refs/notes/git-issues/" + Generation

	// The layout before the generation existed.
	Gen0OpenPrefix   = "refs/issues/"
	Gen0ClosedPrefix = "refs/closed/"
	Gen0NotesRef     = "refs/notes/issues"
)

// Tracking refs are a clone's copy of what a remote held at the last fetch, as
// refs/remotes/ is for branches. They are what makes ahead, behind and diverged
// local questions, and what a lease on a push is taken against. They are never
// pushed: push names the issue prefixes, not the namespace root.

// TrackingOpenPrefix and TrackingClosedPrefix are where a remote's issue refs
// are kept.
func TrackingOpenPrefix(remote string) string {
	return nsRoot + "/remotes/" + remote + "/open/"
}

func TrackingClosedPrefix(remote string) string {
	return nsRoot + "/remotes/" + remote + "/closed/"
}

// TrackingGen0OpenPrefix and TrackingGen0ClosedPrefix are where a remote's gen0
// refs are kept: an unmigrated remote's issues, or what a client older than this
// generation has pushed to a migrated one.
func TrackingGen0OpenPrefix(remote string) string {
	return nsRoot + "/remotes/" + remote + "/gen0-open/"
}

func TrackingGen0ClosedPrefix(remote string) string {
	return nsRoot + "/remotes/" + remote + "/gen0-closed/"
}

// TrackingNotesRef and TrackingGen0NotesRef are a remote's reverse indexes.
func TrackingNotesRef(remote string) string {
	return "refs/notes/git-issues/remotes/" + remote + "/" + Generation
}

func TrackingGen0NotesRef(remote string) string {
	return "refs/notes/git-issues/remotes/" + remote + "/gen0"
}
