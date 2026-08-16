# logd: a positional write is checked for existence, not identity, so a concurrent insert or delete before the index silently retargets it

A positional write is checked against the array when it is submitted and again when it
commits, and both checks ask the same question: is there an element at this index. Neither
asks whether it is the SAME element, so a concurrent insert or delete before the index
silently retargets the write.

## The window

```
client A reads votes = [scott, dee, ana], decides to approve dee, submits votes[1]
client B deletes votes[0]                                   -> [dee, ana]
client A commits                                            -> votes[1] is ana
```

votes[1] exists at both check points, so both pass. A's approval lands on ana's vote. No
error is reported to anyone, and nothing in the log records that the write meant something
else.

Deleting the element A named is caught -- that is what the commit-time check is for
(7cdvym1fh12ksmd5g5n0). Moving it is not.

## Why it was left

Deliberate, and recorded here rather than in a comment: the existence check closes the hole
that made the whole log unreadable, and this one loses a single write to a race. They are
different sizes and the first should not wait for the second.

## Shapes of a fix

- **Capture the element at submit and compare it at commit.** Exact, and it is what the
  client actually meant -- "the element I was shown". Costs one node kept per patcher and one
  comparison under the commit lock, where the head already serves the read. It is a CAS
  precondition in all but name, so it may be better spelled as one.
- **Say that positional writes are not identity-stable, and give an identity spelling.** A
  keyed array (`!logd-key`) has one already: `votes(dee).choice` names dee's vote wherever it
  sits. That is the real answer for anything durable, and this issue is then a documentation
  and API-guidance one rather than a check.

The first is cheap and narrow; the second is what the store already wants callers to do.

Seen on main at 30817c5 (and after the checks landed).