# diff/patch: a composed tag does not round trip -- Patch(a, Diff(a,b)) != b for !t1.key(name)

A node whose tag COMPOSES a data tag over an operator -- `!t1.key(name) [...]` -- does not survive
Patch(a, Diff(a, b)) == b. Some of those patches error outright.

Found by the diff/patch property generator once it could produce such documents at all. Until
c8d9317 it rendered them as `!t1 !key(name) [...]`, which the parser accepted while dropping the
!t1, so every one of these shapes was silently outside the test's reach.

## What happens

With the generator composing (the change is two lines, see below), seeds fail in two ways:

    seed 6:   round trip did not arrive at b
              left: !retag(t1.t1.key(name),t1.key(name)) null
              and:  patched result has "!bracket" twice in one tag

    seed 188: Patch failed
              diff: !arraydiff.retag(bracket,bracket.t1)
                0: !arraydiff.retag(bracket,bracket.t2)
                  ...
                  4: !rmtag(t1) null

The first was a duplication in patch.go's tag restoration and is FIXED in c8d9317: the guard asked
whether the result held the whole composed preTag, ir.TagHas answers per label, so it never
matched and composed again. What remains is the second: composed tags through !arraydiff, !retag
and !rmtag.

## Reproducing

In go-tony/diff_patch_property_test.go, gnode.renderTo currently emits the key tag alone for a
keyed node. Compose the data tag over it instead:

    tag := n.tag
    if n.kind == "keyed" {
        keyTag := "!key(" + n.keyPath + ")"
        if tag == "" {
            tag = keyTag
        } else {
            tag = ir.TagCompose(tag, nil, keyTag)
        }
    }

and run `go test . -run Property`. The generator is deliberately scoped back to the uncomposed
form in c8d9317, with a comment pointing here, so the suite says what it covers rather than
running red or passing quietly.

## Why it matters

Composed tags are not exotic. !bracket composes onto everything the parser reads with brackets,
!key rides on every keyed list, and any data tag a document carries composes with both. The
storage paths diff and patch materialized state constantly, and logd's overlays and stepping are
built on Patch(a, Diff(a,b)) == b holding -- see verify-patches-by-applying in the head/overlay
code. A shape that does not round trip there is a shape that corrupts a store quietly.

## Where to look

  - patch.go restoreTag, which now handles composition on the way OUT of an op
  - libdiff MakeTagDiff and Reverse, which build !retag/!addtag/!rmtag from whole tag strings and
    appear to assume a single label
  - the !arraydiff path, where the failing seeds compose a retag onto the array's own tag