# kpath: should '_' be accepted as a wildcard alongside '*'?

A question to resolve, not a defect. Filing it with what is true today so the decision is
made once and written down.

WHERE THINGS STAND, measured rather than recalled:

    $ printf 'a: {b: 1, c: 2}\n' > w.tony
    $ o get 'a.*' w.tony
    error executing get: any field .* in get          <- the grammar has it, get refuses it

    $ printf 'a: {_: 1, c: 2}\n' > u.tony
    $ o get 'a._' u.tony
    1                                                  <- _ is an ordinary field name

    $ printf 'a: {"*": 1}\n' > s.tony
    $ o get 'a."*"' s.tony
    1                                                  <- a literal star needs quoting, and works

So: `*` is the wildcard in the path grammar (KPath.FieldAll, and [*] {*} for the two array
kinds), a field literally named `*` is reachable by quoting it, and `_` means nothing
special -- it is a field name like any other, and one that occurs in real data.

THE QUESTION. Should `.\_` also parse as "any field", so that a caller may write either
spelling?

WHAT ARGUES FOR IT

  - `_` is the wildcard in several languages a reader of this may carry habits from (Go's
    blank identifier, pattern matches in ML/Rust/Scala, Prolog). "I do not care what this
    is" reads naturally.
  - `*` has a second life as a shell glob, so a path written on a command line has to be
    quoted to survive the shell -- `o get 'a.*'` -- which is a papercut every user meets
    once and some meet repeatedly. `_` needs no quoting anywhere.
  - The two array wildcards are bracketed ([*], {*}) and so are shell-safe already; it is
    only the FIELD wildcard that collides.

WHAT ARGUES AGAINST IT

  - `_` is a legal field name TODAY and it works, as above. Making it a wildcard silently
    changes the meaning of every existing path that names such a field -- and unlike a
    parse error, the new meaning is a successful query returning something else. That is
    the shape of failure this repo has been paying for all month (k0d4y1m6h12kr7cdgdn0: a
    path that is misread rather than refused).
  - `_`-named and `_`-prefixed keys are common in stored data (`_id`, `_type`, `_`), rather
    more common than `*`-named ones, so the collision is not hypothetical.
  - Two spellings for one thing is a cost in itself: every reader has to learn both, every
    writer picks one, and the two look different in a log, a stored index path and an
    error message. The index stores paths as strings.
  - The escape hatch exists (`a."_"`) but only helps a caller who KNOWS the collision.

WHAT WOULD HAVE TO CHANGE IF THE ANSWER IS YES

  - kpath.Parse and SegmentString: parse both, and choose ONE to render, since a path is
    compared and stored as a string (index segments, LogSegment.KindedPath, watch paths). If
    both spellings render as themselves, two paths that mean the same thing compare unequal
    -- which the index would treat as two different paths.
  - token.KPathQuoteField: quote a field literally named `_`, as it already quotes one named
    `*`, or the round trip loses it (this is the family of the panic in 0w79k6hqh12krgcwgdn0
    and the write corruption in r05ms7nch12ksxttgdn).
  - a migration note: any stored path naming a field `_` is now a wildcard, and stored paths
    are in index.gob and in every watch a client re-establishes.

RECOMMENDATION, for whoever decides: no. The shell papercut is real but it is one pair of
quotes, and it is paid by the person writing the command, who sees the result. The collision
is paid by whoever stored a field called `_`, who does not. If the papercut is worth fixing,
fix it without a second wildcard -- an unquoted `.*` at the END of a path is unambiguous to a
shell only when it matches no file, which is exactly when it does not need quoting, so the
better fix may be in the shell-facing tools rather than in the grammar.

Related: 5mw4wcxmh12ksd25g5n0 (ergonomic delimiters other than '.'), which is the same
question about a different character and should probably be decided with this one.