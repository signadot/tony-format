# tony-codegen: a pointer-to-slice field is unsupported, so absent/empty/present cannot be told apart without a hand-written codec

go-tony v0.0.129, hit in verse (github.com/signadot/verse) while designing a
charter grammar field.

## What happens

A `*[]T` field on a type carrying `//tony:schemagen=…` fails generation:

    //tony:schemagen=verse-launch-spec,notag
    type Spec struct {
        ...
        Probe *[]string `tony:"field=probe,omitzero"`
    }

    $ go tool tony-codegen -dir .
    Processing package: launch
    failed to process package ".": failed to generate code:
      failed to generate FromTonyIR() for "Spec":
      failed to generate field decoding for "Probe":
      unsupported pointer to primitive type: slice

Plain `[]T` generates fine; `*T` for a struct generates fine (Spec.Provider is
one). It is specifically pointer-to-slice.

## Why I wanted one

A three-state list field: ABSENT (no opinion, take the default), EMPTY (an
explicit "none of them"), and PRESENT. With a plain slice the three collapse to
two, in whichever direction the tag chooses:

  - with `omitzero`, an empty slice is not emitted at all, so EMPTY arrives as
    ABSENT — the same document means different things on either side of a wire.
  - without `omitzero`, nil and empty BOTH emit `[]` and both decode back as
    empty, so ABSENT arrives as EMPTY.

Measured both, round-tripping through gomap.ToTonyIR → encode → parse →
gomap.FromTonyIR. A pointer is the ordinary Go answer to exactly this, and it is
the answer this package already uses for the same question elsewhere (Grant is a
pointer "for the same reason NamedGrant returns an ok: the zero Grant permits
NOTHING, so 'no envelope declared' and 'the empty envelope' must not be the
same").

The escape is `codec=custom` plus a hand-written FromTonyIR/ToTonyIR — which for
this case would have meant two of them, since the field lives on both a charter
type and its runtime twin, plus the curated schema entries that go with them.
That is a lot of upkeep for one tri-state field, so we changed the field's shape
instead (a comma-separated word list in a string).

## Two things, either of which would have helped

1. Support `*[]T`: emit the slice decoder, allocate on presence, leave nil on
   absence. Symmetric on the encode side — nil omitted under `omitzero`, an
   empty slice emitted as `[]`.

2. If it is unsupported by design, say so in the error and name the escape:
   "pointer to slice is not supported; declare codec=custom and write the codec"
   is actionable where "unsupported pointer to primitive type: slice" reads like
   an internal detail. (`[]T` is also not a "primitive type", which sent me
   looking in the wrong place first.)

## Diagnostic note, worth its own line

The failure is package-scoped and the reason does not survive `go generate`:

    $ make generate      # go generate ./...
    Processing package: launch
    launch/launch.go:28: running "go": exit status 1
    make: *** [generate] Error 1

Nothing about pointers, nothing about which field. You have to run tony-codegen
directly to see the cause. Aborting the whole package is the right call — a
partially generated codec would be worse — but the exit status is all that
reaches the caller, and a build that regenerates several packages will report
only that one of them failed.