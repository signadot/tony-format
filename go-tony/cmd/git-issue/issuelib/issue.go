package issuelib

import (
	"time"
)

// Issue is an issue's metadata: everything about it that is not prose. The
// prose -- description and comments -- lives in the same tree as files, since
// markdown in a text editor and markdown in a Tony string are not the same thing
// to read or to diff.
//
// The tagged fields are exactly the contents of meta.tony, whose codec is
// generated from this struct (see issuelib_gen.go); adding a field here and
// regenerating is the way to extend the format. Ref and Title carry no tag: they
// are recovered on read from where the issue was found and from the first line
// of description.md, so they cannot fall out of step with it.
//
//tony:schemagen=issue
type Issue struct {
	ID      string    `tony:"field=id"`      // XIDR, unique across clones
	Status  string    `tony:"field=status"`  // "open" or "closed"; the ref namespace is authoritative
	Created time.Time `tony:"field=created"` // set once, at create
	Updated time.Time `tony:"field=updated"` // bumped by Store.Update on every edit

	Commits  []string `tony:"field=commits"`             // full SHAs linked to this issue
	Branches []string `tony:"field=branches"`            // branch names; no command sets these yet
	ClosedBy *string  `tony:"field=closed_by, optional"` // SHA of the commit that closed it, if given

	// Relations to other issues, by XIDR. Blocks/BlockedBy are maintained as a
	// pair -- recording one writes the other issue's opposite field -- so the
	// graph reads the same from either end.
	RelatedIssues []string `tony:"field=related_issues"`
	Blocks        []string `tony:"field=blocks"`
	BlockedBy     []string `tony:"field=blocked_by"`
	Duplicates    []string `tony:"field=duplicates"`

	Labels []string `tony:"field=labels"`

	// Derived on read, not serialized in meta.tony.
	Ref   string `tony:"-"` // git ref the issue was found at, e.g. "refs/issues/abc123..."
	Title string `tony:"-"` // first line of description.md, "# " stripped
}
