# logd: a !raw-wrapped !let is applied as a patch on read, leaving every entity in the store unreadable

Hit on signadot/verse's staging verse. Every entity read failed for ~4 minutes
after ONE write; removing that entity restored the store completely, with no
data lost.

## Symptom

    $ verse entity get git:head:verse
    entity read failed: read git:head:verse: match error: storage_error:
      failed to read state: failed to apply patches:
      let patching "null" gave cannot patch with let operation

Not just the entity that was written -- EVERY entity. Listing returned `[]`
rather than erroring, which is how it first read as "staging lost its data".

## What was written

A verse charter rule whose condition carries a `!let` match operator:

    condition:
      subject:
        system: merge
        kind: proposal
        value: !let
          let: [{tip: !get-path {system: git, kind: head, id: verse, path: sha}}]
          in:  {state: open, base: !not .[tip]}

verse stores a rule under `!raw` precisely because a write is a patch and a
charter is full of match operators. Its own write guard agrees and is working as
designed -- `entity.CheckPatchable` refuses `!let`, `!glob` and `!irtype` in a
bare payload, and passes all three under `!raw`:

    {value: !let {...}}          → refused: "`!let` is a MATCH operator and cannot be written"
    the same under !raw          → allowed

So the write was correct by the contract `!raw` states: the subtree is data,
nothing beneath it is interpreted. The store then interpreted it.

## The contrast that makes it specific

`!glob` under `!raw` is stored in ANOTHER rule on that same store right now and
has been for days:

    merge-proposal → condition.subject.value.name: !glob "verse/auto/*/*"

Same escape, same store, no trouble. It is `!let` that gets applied.

## What does NOT reproduce it

A fresh verse on a private data dir, same binary, same go-tony (v0.0.133),
installing the identical rule:

  - read the rule back: fine, tags intact
  - read unrelated entities: fine
  - push the log to 1200+ commits so logd snapshots (server log shows the
    snapshot policy firing), then read again: still fine

So it is not the rule text and not the operator alone. Something about that
store's state, or about which build the store process is running: verse deploys
one image in two roles (`docd` runs `o`, `versed` runs `verse`), so they should
match -- but a docd pod that did not roll with the last ship would be running an
older `o`, and that is the first thing to check on our side.

## Recovery, for anyone who hits it

Deleting the offending entity restored materialization immediately:

    verse trigger remove proposal-outrun     # the rule that carried the !let

Reads came back at once, every entity intact, storeRev still advancing. That is
better than the `!irtype` occurrences we had before, which needed the store
wiped.

## Why it is worth fixing rather than avoiding

`!raw` is the ONE escape that lets a document containing operators be stored as
data, and a charter is exactly such a document. If it holds for `!glob` and not
for `!let`, then it holds by coincidence of which operators have been tried --
and the failure is not local to the bad entity: one unapplicable patch makes the
whole store unreadable, so a single write takes the verse down for reads.