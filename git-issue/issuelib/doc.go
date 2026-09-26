// Package issuelib is the storage layer for git-issue: it keeps issues in the
// git repository itself, as refs, and gives the commands package a Store to
// read and write them through.
//
// # Storage model
//
// An issue is a ref pointing at a commit chain. Open issues live under
// an open namespace, closed ones under a closed one, both keyed by the issue's
// XIDR and both under a generation that says which ref layout this is
// (namespace.go):
//
//	refs/git-issues/v1/open/<xidr>    an open issue
//	refs/git-issues/v1/closed/<xidr>  a closed issue
//	refs/notes/git-issues/v1          reverse index, commit -> issue IDs (see Store.AddNote)
//
// Closing an issue moves the ref rather than rewriting it, so history is
// preserved and the two namespaces are the whole of an issue's status. Every
// accessor that takes an ID searches both, which is why a link to an issue keeps
// resolving after it closes.
//
// The tree under an issue's ref holds the content:
//
//	description.md               title (first line) and body
//	meta.tony                    the Issue struct, in Tony format
//	discussion/<ts>-<hash>.md    one comment, content-addressed
//	discussion/files/...         attachments, original layout preserved
//
// Each edit appends a commit to the chain, so "git log <the issue's ref>" is
// the issue's audit trail and no edit loses what came before.
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
//   - github.com/signadot/tony-format/git-issue/commands - the CLI built on Store
//   - github.com/signadot/tony-format/go-tony/gomap - struct <-> Tony conversion for meta.tony
package issuelib
