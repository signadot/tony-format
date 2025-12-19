# Store project coordination files in issue tracker

# Store project coordination files in issue tracker

## Problem

Files like CLAUDE.md, design documents, implementation plans, and discussion notes currently live in the working tree where they:
- Clutter the main repository for 3rd party users
- Mix workflow/meta content with production code
- Create noise in git status and diffs
- Don't have a clear organizational structure

## Proposal

Use the git-issue tracker itself to organize these files:

1. **Project coordination issue** - A permanent "meta" issue that holds:
   - CLAUDE.md (instructions for AI agents)
   - Project conventions and standards
   - Workflow documentation

2. **Design discussions** - Each design stored as an issue with:
   - Design document as description or attachment
   - Comments tracking evolution of the design
   - Links to commits that implement it

3. **Feature planning** - Issues that track:
   - Implementation plans
   - TODOs and progress tracking
   - Related design files and notes

## Benefits

- **Clean working tree** - No meta files in production repo
- **Organized by topic** - Each issue is a focused discussion
- **Full history** - Git log shows evolution of discussions
- **Distributed** - Syncs via git push/pull like code
- **Accessible** - `git issue show 1` to see coordination files
- **Linkable** - Reference issues from commit messages

## Implementation Ideas

- Add `git issue export <id> <path>` to extract attachments to working tree
- Add `git issue import <id> <path>` to update issue from working tree files
- Consider special issue #000000 or #999999 for permanent coordination
- Add `git issue cat <id>:<path>` to view files without extracting

## Example Workflow

```bash
# Create coordination issue
git issue create "Project coordination and AI agent instructions"
git issue attach 1 ./CLAUDE.md
git issue attach 1 ./CONVENTIONS.md

# Later, update from working tree
git issue export 1 CLAUDE.md     # Extract to edit
# ... edit CLAUDE.md ...
git issue import 1 CLAUDE.md      # Update issue
rm CLAUDE.md                      # Clean working tree

# Or view directly
git issue cat 1:CLAUDE.md
```

## Questions

1. Should there be a special "permanent" issue number for coordination files?
2. Should `export`/`import` commands be added, or is `attach` + manual `git show` sufficient?
3. How to handle updates to coordination files - new attachment or update existing?