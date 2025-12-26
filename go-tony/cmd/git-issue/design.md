# Git-Native Issue Tracker Design

## Goals

1. Track issues/tasks/decisions that span commits and branches
2. Close issues on commit while keeping them accessible
3. Link issues to commits/branches (bidirectional)
4. Organize discussion files and artifacts per issue
5. No external services, everything in git

## Non-Goals

- Issue tracker bridges (GitHub, GitLab, etc)
- Complex workflows (milestones, sprints, etc)
- Full-text search
- Web UI (CLI only for now)

## Storage Model

### Issue Storage

Each issue is stored as a git ref with commit chain:

```
refs/issues/001      # Open issue
refs/issues/002      # Another open issue
refs/closed/003      # Closed issue (still accessible)
```

Each commit in the chain represents an operation (create, update, link, close):

```
refs/issues/001
  ↓
Commit: "create: implement streaming processor"
  Tree:
    meta.tony
    description.md
  ↓
Commit: "link: abc123def (initial implementation)"
  Tree:
    meta.tony         # Updated with commit link
    description.md
  ↓
Commit: "discuss: add integration notes"
  Tree:
    meta.tony
    description.md
    discussion/
      2025-12-19-integration.md
      diagrams/
        flow.svg
  ↓
Commit: "label: add 'streaming' label"
  Tree:
    meta.tony
    description.md
    labels.tony       # New file
    discussion/...
```

### Reverse Index (Commit → Issues)

Use git notes to track which issues link to each commit:

```
refs/notes/issues
  Notes on commit abc123def: "001 005"
  Notes on commit def456abc: "003"
```

### Sequential ID Allocation

Store next ID in a ref:

```
refs/meta/issue-counter → blob "4\n"
```

Increment atomically when creating issues.

## File Formats

### meta.tony

```tony
id: 1
status: open          # or: closed
created: 2025-12-19T10:00:00Z
updated: 2025-12-19T10:00:00Z
commits: [            # Linked commits (optional)
  abc123def456
  def456abc123
]
branches: [           # Linked branches (optional)
  tip/streaming-processor
]
closed_by: null       # Commit SHA when closed, or null
```

### description.md

Plain markdown describing the issue:

```markdown
# Implement streaming patch processor

## Context
Need to apply patches while streaming events without loading full document.

## Approach
See docs/streaming_patch_processor.md for full design.
```

### labels.tony (optional)

```tony
labels: [
  bug
  performance
  streaming
]
```

### discussion/ directory (optional)

Arbitrary files organized by date or topic:

```
discussion/
  2025-12-19-initial-thoughts.md
  2025-12-20-integration-notes.md
  2025-12-21-performance-results.tony
  diagrams/
    architecture.svg
    flow.png
```

## Commands

### Create Issue

```bash
git issue create "Implement streaming processor"
# Creates refs/issues/001 with initial commit
# Opens editor for description
# Increments issue counter
```

### List Issues

```bash
git issue list
# Lists open issues, sorted by updated (newest first)

git issue list --all
# Includes closed issues

git issue list --label streaming
# Filter by label
```

### Show Issue

```bash
git issue show 001
# Displays:
# - meta.tony content
# - description.md
# - labels (if any)
# - discussion/ tree
# - linked commits/branches
```

### Link Issue to Commit

```bash
git issue link 001 abc123def
# Adds commit to meta.tony commits list
# Adds issue to git notes for that commit
# Creates new commit in issue chain

git issue link 001 --branch tip/streaming
# Links to branch
```

### Add Discussion

```bash
git issue discuss 001 notes.md
# Prompts for content or reads from file
# Adds to discussion/ directory
# Creates new commit in issue chain

git issue discuss 001 --file diagrams/flow.svg
# Adds binary file
```

### Label Issue

```bash
git issue label 001 streaming performance
# Sets labels in labels.tony
# Creates new commit in issue chain

git issue label 001 --remove bug
# Removes label
```

### Close Issue

```bash
git issue close 001
# Moves ref: refs/issues/001 → refs/closed/001
# Updates meta.tony status to closed
# Optionally records closing commit

git issue close 001 --commit def456abc
# Records which commit closed it
```

### Reopen Issue

```bash
git issue reopen 001
# Moves ref: refs/closed/001 → refs/issues/001
# Updates meta.tony status to open
```

### Query by Commit

```bash
git issue for-commit abc123def
# Uses git notes to find linked issues
# Shows issue summaries
```

## Commit Message Integration

Support auto-closing issues via commit message patterns:

```bash
git commit -m "Implement streaming processor

Closes #001
See #005"

# Hook detects patterns:
# - "Closes #NNN" → closes issue NNN
# - "See #NNN" → links to issue NNN (doesn't close)
```

Hook location: `.git/hooks/post-commit`

## Implementation Notes

### Concurrency

Issue creation requires atomic ID allocation:
- Read refs/meta/issue-counter
- Increment
- Create refs/issues/NNN
- Update counter

Use git's atomic ref updates to handle races.

### Merging

When pulling/merging:
- Issue refs merge like branches (git handles this)
- Git notes have built-in merge strategy
- Manual conflict resolution if needed

### Remote Sharing

Push/pull issues to remotes:

```bash
git push origin 'refs/issues/*' 'refs/closed/*' 'refs/notes/issues'
git fetch origin 'refs/issues/*:refs/issues/*' 'refs/notes/issues:refs/notes/issues'
```

Can add to `.git/config`:

```
[remote "origin"]
  fetch = +refs/issues/*:refs/issues/*
  fetch = +refs/closed/*:refs/closed/*
  fetch = +refs/notes/issues:refs/notes/issues
  push = refs/issues/*
  push = refs/closed/*
  push = refs/notes/issues
```

### Storage Size

Each issue is a commit chain. Small metadata changes create small commits.
Discussion files are stored as blobs (deduplicated by git).
Efficient for typical usage (dozens to hundreds of issues).

## Tool Integration

### As Go Tool Dependency

In `tools.go`:

```go
//go:build tools

package tools

import (
	_ "github.com/signadot/tony-format/go-tony/cmd/git-issue"
)
```

Install with:

```bash
go install github.com/signadot/tony-format/go-tony/cmd/git-issue
# Now available as: git-issue or git issue
```

### Git Alias

Git automatically finds executables named `git-*` on PATH:

```bash
git issue list
# Calls: git-issue list
```

## Example Workflow

```bash
# Create issue
git issue create "Implement streaming processor"
# Creates refs/issues/001

# Work on feature
git checkout -b tip/streaming
# ... make changes ...
git commit -m "Initial streaming processor implementation

See #001"
# Hook links commit to issue 001

# Add discussion notes
git issue discuss 001 <<EOF
## Integration Notes
The event collection logic is tricky - see streaming_processor_integration.md
EOF

# Add label
git issue label 001 streaming

# Complete implementation
git commit -m "Complete streaming processor

Closes #001"
# Hook closes issue, moves to refs/closed/001

# Later: find issues for a commit
git issue for-commit abc123
# Shows: Issue #001: Implement streaming processor (closed)

# View closed issue
git issue show 001
# Still accessible even though closed
```

## Status

- Design: Complete
- Implementation: In progress
- Location: `cmd/git-issue/`
