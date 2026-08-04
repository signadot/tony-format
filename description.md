# logd: a conditional patch is 60–500x slower than an unconditional one, and it is not the match's anchor

`PatchIf` costs two orders of magnitude more than `Patch`, and the cost does not come from
where the match is anchored or from how big the match is.

Measured against an in-process logd over a temp dir, one libctl session, no other client —
150 writes per arm, each arm writing the same three-field object to the same paths, varying
only the precondition (repro attached, `go run .`):

| entities in the document | Patch (no match) | PatchIf, match at the entity's own path | PatchIf, match at the root |
|---|---|---|---|
| 100 | 1718/s | 24/s | 15/s |
| 500 | 1776/s | 4/s | 3/s |
| 1000 | 1503/s | 17/s | 13/s |

Three things stand out, and the second is the one I cannot explain:

- **The unconditional path is flat.** ~1500–1800/s whatever the document holds, which is what
  you would hope for.
- **The conditional path is erratic, not monotonic.** 500 entities is worse than 1000 — 270ms
  per write against 58ms. A cost that rose with document size would just be an O(size) match;
  this looks like something else, and the per-op figures (40ms, 270ms, 58ms, 65ms, 337ms,
  75ms) are large enough and lumpy enough to suggest waiting rather than computing.
- **The anchor barely matters.** Matching a two-level nested pattern at the root and matching
  a three-field pattern at the entity's own path cost the same order. So this is not "the
  match walks the whole document" — or if it is, it walks it either way.

What this costs downstream: every compare-and-swap verse issues is a PatchIf, and until
today its source reflector used one per entity written. A first pass over a repository
reflected as ~3400 entities took upwards of forty minutes; the same pass with the
preconditions dropped (the slice has a single writer, so the CAS was defending against a
writer that cannot exist) takes ten seconds — 1.2 writes/second against 337. The verse-side
fix is in, so nothing is blocked on this; it is filed because a 60–500x gap between two
neighbouring operations is worth someone knowing about, and because the erratic profile
suggests the cause is not the obvious one.

Not measured here, and worth doing next: whether it is the match evaluation or the commit
path (a conditional commit taking a different route than an unconditional one), and whether
a failing precondition costs the same as a holding one.

Repro attached: a single-file Go program that starts logd, fills a document, and times the
three arms. `go run . -n 500 -sizes 1000,5000` to push it further.