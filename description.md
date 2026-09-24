# git-issue: no edit command — changing a title or body means export, hand-edit, import --force

There is no `git issue edit`. `create`, `comment`, `label`/`unlabel` and `close`/`reopen` cover
everything except changing what an issue SAYS, so correcting a title or rewriting a body — the
ordinary case for a plan issue whose open questions get decided — takes three commands and a
file layout the user has to know:

```
git issue export <id> <dir>        # writes <dir>/description.md, meta.tony, .git-issue
$EDITOR <dir>/description.md       # the first `# ` line is the title
git issue import --force <dir>
```

It works, and discussion, labels and meta survive it. But `--force` is a flag whose name says
"overwrite whatever is there", used for a routine edit, and a script doing the round trip has to
know that `description.md` comes back without a trailing newline if it edits by string match
(one did, and the failed match left the import writing back the unchanged body).

Wanted: `git issue edit <id>` opening `$EDITOR` on the description (title as the first `# ` line,
as `create` does), and the non-interactive forms `create` and `comment` already have — `-b/--body`,
or stdin when it is not a TTY — plus perhaps `--title` alone. An edit racing a pulled change should
be refused rather than clobber it, as the local store's compare-and-swap refuses a write elsewhere.

Source: the command table in `go-tony/cmd/git-issue/commands/` (`export.go`, `import.go`,
`comment.go` for the stdin/editor convention).

Context: found while recording a decision in a verse plan issue (verse `ezx7sarph`), where the
only way to update the body was the export/import round trip.