# o match: exits 0 whether or not anything matched, so a pipe cannot tell a failed pattern from an empty result

`o match` exits 0 whether or not anything matched, and writes nothing when nothing does. A script
therefore cannot tell "the pattern did not match" from "the pattern matched and the result was
empty" from "the command did nothing at all", which is the first thing anyone reaches for in a
pipe.

Measured:

    o m '{state: open}' list.tony          # no output   exit 0   (nothing matched)
    o m '!subtree {state: open}' list.tony # the list    exit 0   (matched)

## Why it is worth fixing beyond taste

This is what makes a wrong pattern look like an empty world. The list-filtering report
(x | o m ... over a list) is mostly about a missing feature, but the reason it is CONFUSING rather
than merely absent is that the failed attempt is silent and successful-looking. An exit code turns
the same session into a diagnosis.

## Proposal

grep's convention, which is what a filter in a pipe is measured against:

    0   at least one document (or element) matched and was written
    1   nothing matched -- not an error, an answer
    2   an error: unreadable input, bad pattern, i/o failure

There is precedent in the tool for an exit code as an answer rather than a fault: `o diff -loop`
exits 1 when the condition asked for did not hold within -loopLim.

Points to settle:

  - `o m` over several files or a --- stream: 0 if ANY document matched, which is grep's reading
  - whether -trim or an eventual -each change it (they should not: they change what is written,
    not whether anything did)
  - whether the same convention should reach `o get` and `o list` for a path that names nothing,
    which is the same question about the same silence