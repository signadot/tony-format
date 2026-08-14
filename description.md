# o patch: a patch that deletes the whole document panics instead of writing the result

`o patch` crashes with a nil dereference when the patch's result is a deletion of the whole
document. Found while trying to hand-roll a list filter out of existing operators, which is how a
user would meet it.

    $ o patch -s '!all !if {if: {state: open}, then: !pass null, else: !delete null}' list.tony
    panic: runtime error: invalid memory address or nil pointer dereference
    [signal SIGSEGV: segmentation violation code=0x2 addr=0x0 pc=0x100cab728]

    goroutine 1 [running]:
    encode.encode(0x0, {...}, ...)   go-tony/encode/encode.go:228
    encode.Encode(0x0, {...}, {...}) go-tony/encode/encode.go:56
    main.patch(...)                  go-tony/cmd/o/patch.go

## Why

`tony.Patch` reports a delete by returning a nil node -- a convention the rest of the tree knows
about and guards, e.g. system/logd/storage/internal/patches/processor.go:157: "A delete leaves
nil, and tony.Patch panics if handed one as the document."

cmd/o/patch.go does not guard it. It passes the result straight to encode.Encode, which
dereferences it.

## What it should do

Deleting the whole document is a legitimate result, so the question is how to WRITE it rather than
whether to allow it. Candidates, in the order I would consider them:

  1. emit nothing and exit 0 -- the document is gone; that is the result
  2. emit `null` -- an empty document is a document, and it round-trips
  3. refuse with "the patch deletes the whole document", which is honest but makes a legal patch
     an error at the CLI only

(1) reads best in a pipe, and pairs with whatever `o m` ends up doing for "nothing matched".

Whatever is chosen, encode.Encode taking a nil node is worth a look of its own: every caller that
forgets is a segfault rather than an error, and the convention that nil means "deleted" makes
forgetting easy.