# git-issue

A git-native issue tracker that stores issues as git refs and uses Tony format for metadata.

## Features

- **Git-native storage**: Issues stored as git refs (`refs/issues/*` and `refs/closed/*`)
- **Sequential IDs**: 6-digit issue IDs (#000001, #000002, etc.)
- **Rich discussions**: Comments, file attachments, and directory trees
- **Commit linking**: Link issues to commits with bidirectional lookup
- **Tony format**: Metadata stored in human-readable Tony format
- **Color-coded status**: Visual distinction between open and closed issues

## Installation

```bash
cd /path/to/go-tony
go build ./cmd/git-issue
# Optionally move to PATH
mv git-issue /usr/local/bin/
```

## Usage

### Create an issue

```bash
git issue create "Issue title"
# Prompts for description (end with Ctrl+D)
```

### List issues

```bash
git issue list           # List open issues
git issue list --all     # List all issues (including closed)
```

### Show issue details

```bash
git issue show 1         # Show issue #000001
```

Output includes:
- Issue title and description
- Linked commits
- Linked branches
- Discussion comments
- File attachments

### Link issue to commit

```bash
git issue link 1 abc123def      # Link by commit SHA
git issue link 1 HEAD           # Link to HEAD
```

Creates bidirectional link:
- Forward: Issue tracks linked commits
- Reverse: Git notes enable commit → issues lookup

### Find issues for a commit

```bash
git issue for-commit HEAD       # Find issues linked to HEAD
git issue for-commit abc123     # Find issues for specific commit
```

### Add comments

```bash
git issue comment 1 "Comment text"     # Add inline comment
git issue comment 1                    # Read comment from stdin
```

Comments are stored as `discussion/NNN.md` files with timestamps.

### Attach files

```bash
git issue attach 1 ./design.md         # Attach single file
git issue attach 1 ./test-results/     # Attach directory tree
```

Attachments stored under `discussion/files/` with preserved directory structure.

### Link issues together

```bash
git issue relate 4 5                   # Link related issues
git issue blocks 4 5                   # Issue 4 blocks issue 5
git issue duplicate 5 3                # Issue 5 duplicates issue 3
```

Creates relationships between issues:
- **relate**: General relationship (umbrella/epic tracking)
- **blocks**: Dependency tracking (bidirectional - updates both issues)
- **duplicate**: Mark duplicates

Perfect for umbrella issues that track multiple sub-issues.

### Close an issue

```bash
git issue close 1                      # Close issue
git issue close 1 --commit abc123      # Close and record closing commit
```

Moves issue from `refs/issues/000001` to `refs/closed/000001`.

## Storage Model

### Git Refs

- **`refs/issues/NNNNNN`**: Open issues
- **`refs/closed/NNNNNN`**: Closed issues
- **`refs/meta/issue-counter`**: Sequential ID allocator
- **`refs/notes/issues`**: Reverse index (commit → issue IDs)

### Issue Structure

Each issue ref points to a commit chain with this tree structure:

```
description.md           # Markdown description (title + body)
meta.tony               # Metadata (ID, status, timestamps, links)
discussion/
  001.md                # First comment
  002.md                # Second comment
  files/
    design.md           # Attached file
    test-results/       # Attached directory
      output.log
      metrics.json
```

### Metadata Format (meta.tony)

```tony
blocked_by: []
blocks: []
branches: []
closed_by: null
commits:
- "abc123def456..."
created: "2025-12-19T10:00:00+01:00"
duplicates: []
id: 1
related_issues:
- "000002"
- "000003"
status: open
updated: "2025-12-19T11:30:00+01:00"
```

## Design Principles

1. **Git-native**: Uses git's object storage, refs, and notes
2. **Distributed**: Works offline, syncs via git push/pull
3. **Human-readable**: Tony format for metadata, markdown for content
4. **Auditable**: Full history via git log on issue refs
5. **Extensible**: Easy to add new fields to meta.tony

## Examples

### Workflow: Feature development

```bash
# Create issue
git issue create "Implement user authentication"

# Work on feature
git checkout -b feature/auth
# ... make changes ...
git commit -m "Add login endpoint"

# Link commits to issue
git issue link 1 HEAD

# Add discussion
git issue comment 1 "Implemented basic auth, need to add OAuth"
git issue attach 1 ./docs/auth-design.md

# Close when done
git issue close 1 --commit HEAD
```

### Workflow: Bug investigation

```bash
# Find issues for a problematic commit
git issue for-commit abc123def

# Add investigation notes
git issue comment 42 "Root cause: race condition in cache"
git issue attach 42 ./debug-logs/

# Link fix commit
git issue link 42 def456abc
git issue close 42 --commit def456abc
```

### Workflow: Umbrella issue tracking

```bash
# Create umbrella issue for major feature
git issue create "Implement user authentication system"

# Create sub-tasks
git issue create "Add login endpoint"
git issue create "Implement JWT tokens"
git issue create "Add password hashing"

# Link sub-tasks to umbrella
git issue relate 10 11    # Link umbrella to subtask
git issue relate 10 12
git issue relate 10 13

# Track dependencies
git issue blocks 11 12    # Login must be done before JWT

# View umbrella issue to see all related tasks
git issue show 10
```

## Implementation Notes

### Why 6-digit IDs?

Provides room for up to 999,999 issues while keeping IDs readable and sortable.

### Why Tony format?

- Human-readable like YAML
- Rich type system (null, arrays, objects)
- Comments and metadata support
- Native Go implementation in this project

### Commit messages

Issue operations create commits with descriptive messages:
- `create: issue 000001`
- `link: abc123d`
- `comment: First line of comment...`
- `attach: filename.txt (3 file(s))`
- `close: closed by abc123d`

### Git notes strategy

Uses `refs/notes/issues` for reverse index. Notes contain newline-separated issue IDs:
```
000001
000042
```

## Limitations

- No built-in web UI (git-native only)
- No email notifications
- No advanced search (use `git log` and `git grep`)
- No user assignment (use git commit authorship)

## Future Enhancements

- Branch linking (partially implemented - field exists but no command)
- Labels/tags
- Milestones
- File export/import commands
- `git issue cat` command for viewing attachments
- GitHub/GitLab bridge

## License

See parent project license.
