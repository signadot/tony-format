# docs: the package docs and pages describe reads, sets and watches as they were before iterType, reads under a commit's schema, and keying_changed

A sweep after 1k6w71sf, 3n390bjw and 62r9amwp found claims left behind:

- api package doc: a match is "the state and the commit" (no sets, no return); an ending always carries "the highest commit accounted for"; no keying_changed or unsupported in the error overview; element names canonicalized "where a request arrives" with no word on a read's commit.
- MatchResult: a one-node answer is "Commit and Body, with Path and Done empty" -- a retspec can ask for more. MatchRequest.Commit said nothing of the schema; Return said nothing of mounts. NewEndedEvent and failWatch: commit is always the highest accounted for.
- Storage.SchemaFor: "what a caller outside the store needs to spell an element's name" -- reads now use SchemaForAt.
- libctl: Handler.Match silent on the runtime refusing a retspec; WatchEndedError's "Commit, the last commit delivered".
- docd: composition.md silent on retspec for composed reads and on keying_changed's commit; doc.go "carries the last delivered commit"; end-reason lists without keying_changed.
- keyed.md has no word on changing a declaration; session.md's invalid_path and unsupported rows; index.md's event-preservation guarantee.
- retention.md links objpath.md#where--may-not-go, which no heading makes (since 7fc7acf4).