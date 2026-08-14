# codegen: a non-pointer container field silently ignores a node of the wrong type, where the reflection path errors

A generated codec drops a field whose node is the wrong type; the reflection path errors on the
same document. So `gomap.FromTonyIR` and the generated `FromTonyIR` disagree about what a
document means, and the disagreement is silent on the side that keeps going.

    doc                      generated FromTonyIR        gomap.FromTonyIR
    plain: hello   ([]string)   Plain=nil,  err=nil       err: expected array, got String
    mp: hello      (map[string]T)  Mp=nil,  err=nil       err: expected object, got String

Measured at go-tony v0.0.131 against a generated codec and the reflection path over the same
struct shape.

## Where it is

The container cases of `generateFieldDecoding` open a guard and have nothing on the other side
of it (go-tony/gomap/codegen/generator.go:2297 for slices, :2427 and :2483 for maps, :2388 for
sparse arrays):

    if fieldNodeUnwrapped.Type == ir.ArrayType {
        ...
    }
    // and otherwise: nothing. The field keeps its zero value and the decode succeeds.

The reflection path refuses it instead (go-tony/gomap/from.go:633).

## Why it is worth a decision rather than a patch

A zero value reads as absent, so the reader cannot tell a field the author omitted from a field
the author got wrong. A type that changed shape between writer and reader -- a list that became
a string, a scalar that became an object -- decodes as "not there", and every default downstream
of it applies as if the author had said nothing. That is the failure mode that does not announce
itself and cannot be found from the reading side.

It is also an inconsistency inside one generator as of v0.0.131: a pointer-to-container field
DOES error on a mismatch, because there the silence was worse still -- the pointer got assigned
whether or not anything decoded, so a mismatch arrived as a non-nil pointer to a nil slice, which
says "the author wrote an empty list". That was fixed in 057e498. The non-pointer case was left
alone deliberately, because it is not new code: it is how every committed codec has always
decoded.

## Why it is not simply "make it error"

The change lands on all 10 packages in CODEGEN_PKGS and everything that reads documents through
them, logd and docd wire paths included. A document that decodes today would start failing, and
that is the point of the change, but it is a decision about existing behaviour and not a bug fix
in isolation:

  - a reader that tolerated an old field shape during a migration would begin refusing it
  - a schema that widened a field would have to be rolled out to readers before writers

## Options

1. Error, matching the reflection path. Codegen and gomap then agree everywhere, and the two
   messages should be the same sentence. One release note, one behaviour.
2. Error under an option -- gomap already has `UnmapOption` (gomap/options.go:63) and the
   generated `FromTonyIR` already takes `opts ...gomap.UnmapOption`, so a strictness flag has a
   place to live and each caller chooses. The cost is that the default stays wrong and the
   divergence stays, just parameterised.
3. Leave it and write it down. Cheapest, and it leaves gomap and codegen disagreeing about the
   same document, which is the thing most likely to be discovered by a bug rather than by
   reading.

I would take (1), on the argument that a codec's job is to say what a document says or refuse it,
and that two paths for the same struct must not answer differently. (2) is the compromise if
existing readers turn out to depend on the leniency; that is worth measuring before choosing.

## What the fix looks like

The mechanism already exists: `mismatch` in go-tony/gomap/codegen/nested.go closes the recursive
emitters' guards with an else that names both types. The one-level paths need the same close, and
the matrix test needs cases that feed a wrong-typed node to every container shape -- the existing
"generated agrees with reflection" test compares only VALID documents, which is why this survived
the work that added it.