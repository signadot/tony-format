// Package issuelib is the storage layer for git-issue: it keeps issues in the
// git repository itself, as refs, and gives the commands package a Store to
// read and write them through.
//
// # Storage model
//
// An issue is a ref pointing at a commit chain. Open issues live under
// refs/issues/, closed ones under refs/closed/, both keyed by the issue's XIDR:
//
//	refs/issues/<xidr>    an open issue
//	refs/closed/<xidr>    a closed issue
//	refs/notes/issues     reverse index, commit -> issue IDs (see Store.AddNote)
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
// Each edit appends a commit to the chain, so "git log refs/issues/<xidr>" is
// the issue's audit trail and no operation loses what came before.
//
// # Identifiers
//
// Issues are named by XIDR, the byte-reversed form of an XID (see the XID type).
// Reversal puts the counter and machine bytes first, so a short prefix -- the
// three or four characters a person actually types -- is already unique; the
// unreversed form would open with a timestamp shared by every issue filed that
// second. Accessors take a full XIDR or any unambiguous prefix of one.
//
// Six-digit numeric IDs from the tracker's first iteration are still recognized
// on read (FormatID, IsLegacyRef, ParseLegacyID) so old refs remain reachable
// until "git issue migrate" rewrites them.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/cmd/git-issue/commands - the CLI built on Store
//   - github.com/signadot/tony-format/go-tony/gomap - struct <-> Tony conversion for meta.tony
package issuelib
