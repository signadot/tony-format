# sources merge by fetch time, so a URL correction loses to a later fetch elsewhere

`mergeSources` (issuelib/sync_ext.go) merges a sources ref edited on both sides by taking whichever record has the later `fetched` time. The time is right for `fetched` itself but wrong for `url`: `AddSource` keeps the old `fetched` when it re-records a source, so a clone that corrects a URL and pushes loses the correction to any clone that merely fetched from the old URL afterwards and pushed.

Scenario: clone A runs `ext add b https://y/b` at 10:00; the record keeps fetched=09:00. Clone C pulls at 10:05, refreshes from the old URL, and pushes a record with the old URL and fetched=10:05. A's next pull merges: theirs is later, A's correction is silently reverted, and `ext list` shows the old URL again.

Found by the review of p76fwh8gh12kr1ppvdn0, whose doc says a wrong URL is "corrected where it is written"; the correction holds locally but does not survive this merge. Predates that change.

Proposal: merge the two fields separately. `fetched` takes the later time, as now. `url` would take the record whose change is more recent, which needs a `recorded` time written by `AddSource` on a URL change, or the ref's commit time; a difference with neither later would be refused and named like an issue edited on both sides.