# docd: a quoted path segment loses its quoting on Patch, so a dotted id is written nested

# docd: a quoted path segment loses its quoting on Patch, so a dotted id is written nested

A kpath whose last segment is quoted because it contains a dot — `verse.demo.probe."probe.dotted"`
— is patched as though the quotes were not there. The value lands at
`verse.demo.probe.probe.dotted`: an object `probe` with a field `dotted`, rather than one node
named `probe.dotted`.

The commit succeeds and reports a revision, so nothing upstream sees a failure. A Match at the
same quoted path — the same string, from the same code — then finds nothing.

## The A/B

Identical client code (verse's `entity.LogdStore`, which renders the path once in `refPath` and
quotes with `token.KPathQuoteField`), pointed at two servers.

Against **plain logd**:

    probe-plain    write committed=true rev=1 | get ok=true
    probe.dotted   write committed=true rev=2 | get ok=true
    probe/slashed  write committed=true rev=3 | get ok=true
    listed: [demo:probe:probe-plain demo:probe:probe.dotted demo:probe:probe/slashed]

Against **docd** (the client face of a docd that verse spawned):

    probe-plain    write committed=true rev=1564 | get ok=true
    probe.dotted   write committed=true rev=1565 | get ok=FALSE
    probe/slashed  write committed=true rev=1566 | get ok=true

and the document shows where the dotted one went:

    $ verse state demo.probe
    probe:
      dotted:
        k: v
    probe-plain:
      k: v
    probe/slashed:
      k: v

A slash in a segment is fine; only the dot is affected, which is what points at the quoting
rather than at the character set.

## What it costs downstream

In verse this is silent in both directions, which is why it took a while to see.

A write answers **204 No Content** — its handler writes, reads back, and treats a read miss as
"it landed; we just cannot show it back" — so `verse state put demo pr a.b '{k: v}'` prints
`null`, exits 0, and stores nothing at the address you named. It also leaves a spurious entity
behind at the truncated name.

A DELETE is a write of nothing at the same path, so it misses in the same way and reports
success. That is how this was found: `verse source remove <src> --forget` counted 721 entities
tombstoned and left 192 standing, every one of them a ref whose id carries a dot —
`git:ref:tony/v0.0.158` and its neighbours, from a mirror of a repository with version tags.
The ownership claims were right and the enumeration was right; the deletes landed at the wrong
address.

## Note for whoever fixes it

Verse's own test suite cannot catch this: `entitytest.Store` opens a store on a bare logd, where
the behaviour is correct, while everything a person runs (`verse up`) goes through docd. So a
regression test for this belongs on the docd side, or verse needs a harness that runs the store
suite against both faces — worth deciding, because the two are not interchangeable today and
verse assumes they are.