# logd: native leases -- liveness-coupled expiry, which retention deliberately is not

logd has no liveness-coupled expiry. Retention ages records out by reading a
timestamp from the record itself, which is the right and the more expressive
answer for *ageing* — but it cannot express "this value exists only while the
client that wrote it is alive". That is a different question, and it is the one
ephemeral registration, membership and leader election are built on.

## Why this is not a retention feature

Retention is strictly more expressive than a TTL on a value, and nothing here
should be read as wanting to replace it:

- a rule expires the items of a container that match a *pattern*, not one value
  with a fixed clock attached;
- age is read from the record's own timestamp, so it is exact and depends on no
  compaction setting — a TTL stamped at write time is an approximation of the
  same thing that the store then has to remember;
- the caller carries the policy and the clock, so a pass is reproducible from
  the log and the commit is the record that it ran;
- expiry is an ordinary delete: watchers see it as a delta with an author, the
  schema applies, and each batch carries a precondition on what the pass read.

A TTL gives up all five to answer a narrower question. What retention genuinely
cannot answer is the liveness one, because a record's timestamp says when it was
written, never whether the writer is still there.

## What a lease is for

Bound to a session, refreshed while the session lives, and when it stops being
refreshed the paths attached to it go. That gives:

- ephemeral registration — a controller, a worker, a replica announces itself at
  a path and the announcement disappears when the process does;
- membership that is correct without a reaper that has to guess;
- leader election, from a lease plus the compare-and-swap precondition logd
  already has.

docd's mount registry and its tombstones are the same shape solved ad hoc, one
layer up. Worth looking at whether a lease is what that should rest on.

## Design questions

- **Where does a lease live?** In the document, so it is readable, watchable,
  time-travelable and covered by the schema — or beside it, as session state? In
  the document is more in keeping with everything else here, and costs a commit
  per grant and per revoke, not per keepalive.
- **What does expiry commit?** An ordinary delete, for the same reason
  retention's is one: watchers apply it, a read at an older commit still shows
  the record, and the schema still gets to refuse it.
- **Whose clock?** Retention's answer is "the caller's". A lease cannot borrow
  that — the whole point is that the *server* notices the client stopped. So
  this is the first clock logd owns, and that should be deliberate rather than
  incidental.
- **Keepalives.** On the session, not as commits. A keepalive that commits makes
  an idle cluster write forever, and ties lease traffic to the snapshot and
  compaction thresholds.
- **Scopes.** A lease granted in a scope, and a baseline lease seen through one.
  Probably: a scope sees baseline's leases and may hold its own, and deleting the
  scope drops the scope's.
- **docd.** A client's lease has to survive routing, and a mount's controller has
  to be able to hold one. The mount protocol is the session protocol inverted, so
  a lease request should route like any other — but the expiry is docd's logd's
  to commit, and a controller that goes away is already a tombstone.
- **Failover.** Relates to nkn7ptxch12kr50r9hmg. On an active/passive holdover
  the new leader's clock has not been watching the keepalives, so every lease has
  to get a grace period on promotion or a failover mass-expires the cluster's
  membership at the worst moment. This is the part most likely to be got wrong,
  and it is worth writing down before the flock lands rather than after.

## Not in scope

Anything that makes a lease a second way to express age-based expiry. If a
record should go because it is old, that is a retain rule.

Related: nkn7ptxch12kr50r9hmg (towards HA logd).