# parse: a comment before a list element is silently discarded

With `ParseComments(true)`, a comment written above an element of a block sequence does not
survive the parse. It is on no node — not in `Lines`, not in `Comment`, at no depth.

    - name: a
      stage: open
    # about rule b
    - name: b
      stage: open

parses to

    Array
      Object name=a
      Object name=b

with no trace of "# about rule b". Observed at v0.0.142.

The comment ABOVE the whole list survives, but it lands on the Array — so in a list of things
it belongs to the document rather than to the first element, which is right, and leaves no way
to comment an element at all:

    # about the whole charter        -> Comment wraps Array   (kept)
    - # inside the item              -> Comment wraps Object  (kept)
      name: review
    ---separated documents           -> Comment wraps each    (kept)

Only the natural spelling is lost. The workarounds are to write the comment INSIDE the item
(`- # …` then the fields, which reads badly) or to split the list into `---` documents.

WHY IT MATTERS NOW. v0.0.142 built the encode half of `comment=`/`lineComment=`, so a struct
can carry its own comments through a decode/re-encode for the first time — which is what verse
needs to stop losing a charter's comments when it stores a rule. A charter file is a LIST of
rules, and a rule's comment is written above it, so the annotation captures nothing for exactly
the shape the feature is for. Per-document charters (`---`) do work.

Round-trip check, in case it helps place the fix: the array case suggests the parser attaches a
pending comment to the next VALUE it opens, and a sequence entry does not count as opening one.