// Package issuelib is the storage layer for git-issue: it keeps issues in the
// git repository itself, as refs, and gives the ops and commands packages a
// [Store] to read and write them through. A program that reads a repository's
// issues directly -- a source reflecting the tracker elsewhere -- reads them
// through a [GitStore] made on the repository's directory ([NewGitStoreAt]), and
// lists refs through the prefixes this package exports rather than naming them.
//
// # Storage model
//
// An issue is a ref pointing at a commit chain. Open issues live under
// an open namespace, closed ones under a closed one, both keyed by the issue's
// XIDR and both under a generation that says which ref layout this is
// (namespace.go):
//
//	refs/git-issues/v1/open/<xidr>            an open issue
//	refs/git-issues/v1/closed/<xidr>          a closed issue
//	refs/git-issues/v1/ext/<source>/<xidr>    another repository's issue, mirrored here (ext.go)
//	refs/git-issues/v1/sources/<source>       where that repository is
//	refs/notes/git-issues/v1                  reverse index, commit -> issue IDs (see Store.AddNote)
//
// Closing an issue moves the ref rather than rewriting it, so history is
// preserved and the two namespaces are the whole of an issue's status. Every
// accessor that takes an ID searches open, closed and ext alike, which is why a
// link to an issue keeps resolving after it closes, and why a mirror resolves
// like an issue of this repository. A mirror is read-only here; its status is
// what its source wrote ([StatusOf]).
//
// The tree under an issue's ref holds the content:
//
//	description.md               title (first line) and body
//	meta.tony                    the Issue struct, in Tony format
//	discussion/<ts>-<hash>.md    one comment, content-addressed (comment.go)
//	discussion/files/...         attachments, original layout preserved
//
// Each edit appends a commit to the chain, so "git log <the issue's ref>" is
// the issue's audit trail and no edit loses what came before. A write that finds
// the ref moved since its read is applied again on the new tip, per file, the
// later writer winning on a file both wrote (GitStore.Update).
//
// Sync (sync.go, sync_ext.go) decides per issue from what both sides hold and
// merges what it can (merge.go); what it cannot is refused and named.
//
// # Identifiers
//
// Issues are named by XIDR, the byte-reversed form of an XID (see the XID type).
// Reversal puts the counter and machine bytes first, so a short prefix -- the
// three or four characters a person actually types -- is already unique; the
// unreversed form would open with a timestamp shared by every issue filed that
// second. Accessors take a full XIDR or any unambiguous prefix of one.
//
// Six-digit numeric IDs from the tracker's first iteration are recognized by
// FormatID, IsLegacyRef and ParseLegacyID.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/git-issue/ops - the operations over Store
//   - github.com/signadot/tony-format/git-issue/commands - the CLI and the MCP server over ops
//   - github.com/signadot/tony-format/go-tony/gomap - struct <-> Tony conversion for meta.tony
package issuelib
