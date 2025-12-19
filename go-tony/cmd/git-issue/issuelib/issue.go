// Package issuelib provides the core library for git-issue.
package issuelib

import (
	"time"
)

//tony:schemagen=issue
type Issue struct {
	ID            int64     `tony:"field=id"`
	Status        string    `tony:"field=status"`
	Created       time.Time `tony:"field=created"`
	Updated       time.Time `tony:"field=updated"`
	Commits       []string  `tony:"field=commits"`
	Branches      []string  `tony:"field=branches"`
	ClosedBy      *string   `tony:"field=closed_by, optional"`
	RelatedIssues []string  `tony:"field=related_issues"`
	Blocks        []string  `tony:"field=blocks"`
	BlockedBy     []string  `tony:"field=blocked_by"`
	Duplicates    []string  `tony:"field=duplicates"`

	// Derived fields (not serialized in meta.tony)
	Ref   string // The git ref (e.g., "refs/issues/000001")
	Title string // From description.md first line
}
