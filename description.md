# git-issue: edit a comment in place, without export and import

A comment on an issue cannot be changed once added. `git issue edit` changes the title and the body; `git issue comment` adds one. The only way to correct a comment today is `git issue export <id> <dir>`, editing the discussion file by hand, and `git issue import --force <dir>`, which rewrites the whole issue to fix one paragraph and asks the person to know the export layout.

The ask: a verb that edits one comment in place, for example `git issue comment <id> --edit <comment> [text]` (or `git issue edit <id> --comment <comment> [text]`), taking the comment by its file name as `show` prints it (`discussion/20261004T101544Z-b6397caa.md`) or by an unambiguous prefix. It should be a commit on the issue's chain as `edit` is, so history keeps what the comment said before, and it should be reachable through the MCP server as well, where `issue_comment` today only adds.

Why it comes up: a comment is where a decision is recorded, so it is the thing one most wants to correct precisely. Twice today a comment on a verse issue (dzxn44gw, the staging plan) needed a sentence changed, and the choice was a second comment that supersedes the first or the export and import round trip. The repository's rule is that the body is edited in place and decisions are comments; comments being append-only cuts against correcting the record where it most matters.

Filed from the verse repository (signadot/verse), 2026-10-04.