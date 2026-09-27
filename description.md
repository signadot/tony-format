# git issue serve: the list takes two seconds a load, five the first

Timed 2026-09-27 on tony-format (69 open issues), `git issue serve` on a fresh server:

```
http://localhost:18942/   200  in 5.37s    (first load)
http://localhost:18942/   200  in 2.23s    (second, the page cached)
```

The list's cache token is a digest over every ref and its commit, read with one `GetRefCommit` per ref (handleIndex): a git process for each issue on every load, cached or not. `Store.Tips` answers every ref with its commit in one call, and is what the watch reads.

With `serve -watch` the list reloads whenever an issue changes, so it pays this each time.

Not fixed; found while fixing rmgg0b9fh12kr235q1n0.