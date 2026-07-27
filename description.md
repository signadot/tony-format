# diff: two shipped corpus cases produce a diff that Patch cannot apply (arraydiff index numbering, !key list)

Found while adding a `Patch(a, Diff(a, b)) == b` check over the existing
`diffTests` corpus, for issue 7f8rsk22h12ks2vscxn0 (`!raw`). Two of those
cases produce a diff which the library cannot apply to `a`. Neither has
anything to do with operators held as data; both are pre-existing, and both
crash rather than error.

## `diffTests[2]` -- arraydiff index numbering

    a: [1, 2, "hello ", "hello", "hellp ", 7, 8]
    b: [2, "hello", "hello ", "hello", 4, 7, 9]

    diff: !arraydiff {0: !delete 1, 4: !replace{hellp,hello}, 6: !insert 4,
                      8: !replace{8,9}}

    tony.Patch(a, diff)
      panic: index out of range [7] with length 7
      mergeop/arraydiff.go patchArrayByIndex

The keys are positions in a merged coordinate space which advances for a
delete, an equal and an insert alike. `DiffArrayByIndex` folds a delete
immediately followed by an insert into one `!replace` at `ri-1`, but still
increments `ri` for the insert, so every key after a folded replace is one
too high. `patchArrayByIndex` advances `fi` once per doc-consuming entry and
runs off the end.

Either the emitter should not consume a slot for the folded insert, or the
applier should track the same gap. I did not want to guess which.

Note also that `mergeop.patchArrayByIndex` and the unused exported
`libdiff.PatchArrayByIndex` are two copies of this loop which have drifted:
the libdiff one has `if fi < n` guards, the live one does not, which is why
this is a panic and not an error.

## `diffTests[9]` -- keyed list

    tony.Patch(a, Diff(a, b))
      panic: invalid memory address or nil pointer dereference
      mergeop/keyed_list.go:84 keyedListOp.Patch -> ir.FromSlice

A nil element reaches `ir.FromSlice`.

## Why it matters

`TestDiff` checks the diff's *text* and that `Reverse(Reverse(d)) == d`. It
never applies the diff, so a diff which cannot be applied passes. The invariant
worth holding is `Patch(a, Diff(a, b)) == b`; `TestDiffTestsRoundTrip` in
`diff_roundtrip_test.go` now holds it over the corpus and skips these two
indices, naming this issue.