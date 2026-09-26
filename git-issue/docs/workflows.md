# Workflows

## A feature

```bash
git issue create "Implement user authentication" --body "Login, then OAuth."
# -> Created issue j2dzt7xph12kswa9esn0

git checkout -b feature/auth
# ... make changes ...
git commit -m "Add login endpoint

Issue: j2dzt7xph12kswa9esn0"

git issue link j2dz HEAD
git issue comment j2dz "Implemented basic auth, need to add OAuth"
git issue attach j2dz ./docs/auth-design.md

git issue close j2dz --commit HEAD
git issue push
```

The commit carries the issue's id as a trailer, so a reader of the log finds
the issue; the link and the close record the commit on the issue, so a reader
of the issue finds the commit.

## A bug

```bash
git issue for-commit abc123def          # what was this commit about?

git issue comment 7k2p "Root cause: race condition in cache"
git issue attach 7k2p ./debug-logs/

git issue link 7k2p def456abc
git issue close 7k2p --commit def456abc
```

## An umbrella

```bash
git issue create "Implement user authentication system" --body "..."   # -> 9xq4...
git issue create "Add login endpoint" --body "..."                     # -> 3mb7...
git issue create "Implement JWT tokens" --body "..."                   # -> 8fc1...

git issue relate 9xq4 3mb7      # the umbrella tracks its parts
git issue relate 9xq4 8fc1
git issue blocks 3mb7 8fc1      # login lands before JWT

git issue show 9xq4             # the whole picture
```

## Across repositories

An issue in `verse` that is about a design in `tony-format`:

```bash
cd ~/src/verse
git issue ext add tony ~/src/tony-format
git issue ext fetch tony <the tony-format issue's full id>
git issue relate <the verse issue> <that id>
git issue show <the verse issue>        # the relation, with tony-format's title
git issue push                          # the mirror goes to verse's origin too
```

From an agent through the [MCP server](mcp.md), `issue_relate` across the two
repositories does the mirroring itself.
