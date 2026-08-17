# match: a projection (!fields) so a caller can ask for the shape it wants, not the whole document

A match answers the state, not what matched. There is no way to say "give me these
fields" -- so a caller wanting a count, a rev or one field of one entity is handed the
whole document and throws the rest away. Proposed: a projection operator, !fields.

WHAT IT WOULD BE. A match names a shape; a projection names what comes back. The
vocabulary has plenty of ways to SELECT (!at(path), !subtree, !has-path, !glob) and no
way to shape the ANSWER:

    !fields [rev, status]          the two fields, of whatever the match was applied to
    !at(verse.meta).fields [rev]   composed, the way !at already composes

Shape is a decision: whether it answers a document of the named fields (a subset in
place, so paths still mean what they meant) or a bare list of values; whether a named
field which is absent is omitted or null; and whether it descends (fields of fields) or
names one level. The first of each is what I would expect, since a subset in place is
the thing a client can navigate with the same paths it already uses.

WHAT IT BUYS, AND WHAT IT DOES NOT. Measured on a 400 KB store, per read:

    replay + materialize   12.0 ms   O(base + deltas)
    encode                  2.9 ms   O(payload)

A projection applied to a materialized document buys the 2.9 ms and the wire -- for a
readiness probe reading 455 KB to learn one number, the wire is the whole cost, so
this is a real win for reporting callers. It does NOT buy the 12 ms: the replay has
already happened. Reading a path cheaply is a different, deeper piece of work
(ap8ddvp2h12krd43gdn0).

Pushed DOWN it is worth more. applyPatchesToBase already streams events into a sink,
so a projection could drop events outside the requested shape as they stream: no node
built for the discarded part and nothing to encode. That saves allocation and encoding
without saving the I/O -- between the 2.9 ms and the 12 ms, and it composes with the
narrow-read work rather than competing with it.

WHY IT IS WORTH HAVING ANYWAY. It is the only one of these that a caller can use
without the store changing shape underneath it, and it is honest about what it is: the
answer's shape, not the read's cost. Filed separately from the narrow read for exactly
that reason -- so that shipping it is not mistaken for fixing the 12 ms.