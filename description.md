# git-issue watch: a close reads "close; closed"

A close is a `close` commit on the open ref and then a move to closed, so `git issue watch` and `issue_watch` report both: the commit's subject and the synthetic `closed` for the move. The move entry exists for status changes with no commit of their own (a pull that brings the ref across), so dropping it outright loses those.

Seen on branch issue-vegmw7bm:

```
j2dzt7xph12kswa9esn0  closed  Implement streaming processor  -- close; closed
```

Punted from vegmw7bm; the same applies to reopen.