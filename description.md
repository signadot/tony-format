# sources merge by fetch time, so a URL correction loses to a later fetch elsewhere

`mergeSources` (issuelib/sync_ext.go) merges a sources ref edited on both sides by taking whichever record has the later `fetched` time. The time is right for `fetched` itself but wrong for `url`: `AddSource` keeps the old `fetched` when it re-records a source, so a clone that corrects a URL and pushes loses the correction to any clone that merely fetched from the old URL afterwards and pushed.

Scenario: clone A runs `ext add b https://y/b` at 10:00; the record keeps fetched=09:00. Clone C pulls at 10:05, refreshes from the old URL, and pushes a record with the old URL and fetched=10:05. A's next pull merges: theirs is later, A's correction is silently reverted, and `ext list` shows the old URL again.

Found by the review of p76fwh8gh12kr1ppvdn0, whose doc says a wrong URL is "corrected where it is written"; the correction holds locally but does not survive this merge. Predates that change.

Proposal: merge the two fields separately, and the URL three-way. The two sides of a diverged sources ref share a merge base whenever the source was ever synced; `url` would be taken from whichever side changed it relative to the base, kept when neither did, and refused when both changed it to different values, named like an issue edited on both sides. `fetched` would take the later time, as now. This answers who changed the URL rather than who wrote later, needs no new field or timestamp, and is how git settles a line. Sides with no common history, as when two clones each ran `ext add` under the same name before either pushed, have no base and would be refused the same way.

Considered and not proposed: a `recorded` time in source.tony written on a URL change, which is a schema addition older binaries would have to tolerate; and the commit time of the change, which is free to store but has to be recovered by walking each side's chain past refresh and merge commits.
