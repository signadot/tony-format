# git-issue: XIDRs minted by one process share a long stem, so ids look alike and can spell words

The displayed id is the XIDR: the 12-byte XID reversed, then base32-encoded (`issuelib/xid.go`).
An XID is `time(4) machine(3) pid(2) counter(3)`; reversing puts the counter first, so that short
prefixes vary and stay unique. They do — but the rest of the leading run does not, and the ids end
up looking nearly identical.

**Why.** Within one process the counter is seeded once and `atomic.Add`s by 1 per id, so only its
low byte moves from id to id; its high bytes, the pid and the machine id are fixed for the life of
the process. In XIDR those fixed bytes sit right after the first ~2 characters, so every id a
process mints collapses to `XX<long fixed stem>`:

- This repo's 2025-12 batch: `t7n7ptxch…`, `szn7ptxch…`, `sbn7ptxch…`, `rqn7ptxch…` … `f3n7ptxch…`
  — all `··n7ptxch12k…`, differing only in the first two characters.
- A current batch elsewhere: `2b5art3x…`, `1f5art3x…`, `2k5art3x…`, `1v5art3x…`, `1q5art3x…`
  — all `··5art3xh12ks…`.

This is not a correctness problem: prefixes stay unambiguous (the first two characters already
distinguish them, and past ~256 ids the next counter byte starts moving too). It is a readability
one, with two edges:

1. **Low glanceable variety.** Two characters do all the work; the eye has to diff a long shared
   stem to tell two ids apart, which is most of what a person reads an id for.
2. **The stem can spell things.** Crockford base32 (the alphabet here, minus I/L/O/U) over a fixed
   stem readily forms words — the recurring `…5art3x…` reads as an unfortunate one, and it rides on
   every id this process makes.

**Options, with the trade-off noted:**
- Mix the counter before placing it (e.g. a cheap bijection/hash of `counter` into the 3 bytes) so
  consecutive ids differ in their high bits too, spreading variety across the leading characters.
  Keeps uniqueness and the 12-byte layout; loses the (currently unused here) property that ids from
  one process sort by creation order within a second.
- Or put a few random bytes first instead of the counter, and keep the counter only for
  within-second tie-breaking. Simpler; same sort caveat.
- Independently, curating the alphabet further (dropping vowels) would cut word-formation, at some
  cost to density and familiarity.

Filed as low: cosmetic, but every id in the tracker wears it.