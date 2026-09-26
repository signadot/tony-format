package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Comment files live at discussion/<ts>-<hash>.md, where <ts> is a sortable UTC
// timestamp (lexical order == chronological) and <hash> is a short digest of the
// stored content. Because the name is content-derived and per-comment, two
// independent clones adding different comments never land on the same path — the
// old count-based discussion/NNN.md scheme collided there and lost a comment on
// merge (and skewed/gapped whenever an attachment was present). An identical
// re-add dedups to the same path, which is harmless.

// commentTSLayout is the sortable UTC timestamp embedded in comment filenames.
const commentTSLayout = "20060102T150405Z"

// commentNamePattern matches an already-migrated comment filename (basename), so
// migration is idempotent.
var commentNamePattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{8}\.md$`)

// commentHeaderRe extracts the timestamp from a comment's leading HTML-comment
// header, accepting both the legacy "<!-- Comment 003 - <ts> -->" form and the
// current "<!-- <ts> -->" form.
var commentHeaderRe = regexp.MustCompile(`<!--\s*(?:Comment\s+\d+\s*-\s*)?(.+?)\s*-->`)

// commentFileName returns the discussion path for a comment made at t whose
// stored bytes are content. The hash is taken over content, so the path is a
// stable content address.
func commentFileName(t time.Time, content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("discussion/%s-%s.md",
		t.UTC().Format(commentTSLayout), hex.EncodeToString(sum[:])[:8])
}

// commentBody renders the stored content for a new comment: a timestamp header
// (RFC3339 with offset, for readability) followed by the text.
func commentBody(t time.Time, text string) string {
	return fmt.Sprintf("<!-- %s -->\n\n%s\n", t.Format(time.RFC3339), strings.TrimRight(text, "\n"))
}

// parseCommentTime extracts a comment's timestamp from its header, handling both
// header forms. ok is false when no timestamp can be parsed.
func parseCommentTime(content string) (t time.Time, ok bool) {
	m := commentHeaderRe.FindStringSubmatch(content)
	if m == nil {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(m[1])
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// isCommentFile reports whether a discussion tree path is a comment file (an .md
// not under discussion/files/, which holds attachments).
func isCommentFile(path string) bool {
	return strings.HasSuffix(path, ".md") && !strings.Contains(path, "/files/")
}

// isMigratedCommentName reports whether a discussion path already uses the
// content-addressed scheme (so a migration can skip it).
func isMigratedCommentName(path string) bool {
	base := path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return commentNamePattern.MatchString(base)
}
