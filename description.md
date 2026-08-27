# diff/patch: the property generator cannot express operator tags as data, nor the operator vocabulary at all

The diff/patch property generator cannot produce a document that carries an operator tag
as DATA, and it cannot produce a pattern or patch drawn from the operator vocabulary. Both
regions have had real defects; neither is reachable by the test that exists to find them.

## what it covers

`diff_patch_property_test.go` generates four node kinds -- scalar, object, array, keyed --
with tags drawn from

    nonOpTags = []string{"", "", "", "!t1", "!t2"}   // a DATA tag, never an op

and asserts Patch(a, Diff(a, b)) == b over 500 seeds. So it exercises DIFF'S OUTPUT
vocabulary -- !arraydiff, !strdiff, !replace, !insert, !delete, !retag/!addtag/!rmtag,
!key -- and nothing else.

## blind spot 1: an operator tag as data

A document may hold a rule, a charter, a patch. libdiff.Escape wraps such a value in !raw
on the way into a delta and the tag is consumed on the way out, so the invariant depends on
escape and unescape being exact inverses -- a step the ordinary case does not have. The
generator never produces one, by construction.

It has cost a store already: 6225etzfh12kr955fxn0, a !raw-wrapped !let applied as a patch
on read, every entity in staging unreadable for four minutes after one write. And b660587
today, where both key-tag and auto-id injection walked into a !raw subtree and wrote a tag
and a field into a document the store does not own.

## blind spot 2: the operator vocabulary

38 operations are registered; the generator emits none of them. Every hand-written operator
touched this week had a defect in it:

  - !let could not patch at all, its binding list expanded an unbound name to null, and a
    binding cycle took the process down with a stack overflow (4e3b7cc)
  - !ir built an ir.Node that no document could hold (264837c)
  - !comment could not match, which the format says it should (a047a47)
  - !dive, !if, !all and four others dereferenced the nothing a delete gives back (9a4e2a3)

None of those is reachable from Diff's output, so none was reachable by the property test.
They were found by reading.

## why this is the gate rather than more reading

Every time the generator's reach widened, it found a real defect within seconds. Composed
tags -- a data tag over a key tag, `!t1.key(name)` -- were unreachable until the parser
stopped accepting `!t1 !key(name)` and silently dropping the !t1; the moment the generator
could express them, seven seeds failed at 20000 and the fix was one line in the keyed differ
(b2abea0). The test was building documents and testing different ones.

## shape of the work

  - let the generator emit an operator tag as data, and assert the round trip still holds
    through Escape/!raw
  - a second property over the registered vocabulary: for each op, generate a document and
    a pattern/patch using it, and assert what must hold for all of them -- a patch applies
    or errors, never panics; a match answers or errors, never panics; an op that errors
    leaves the document untouched. !let's cycle and !dive's nil deref are both that shape.
  - the seed count is 500 in CI and the composed-tag defect needed 20000 to show seven
    failures; worth deciding what the committed budget buys.

## why it matters beyond the format

fdf4bdd's lowering makes the stored delta `Diff(before, after)`, so store correctness
becomes exactly Patch(a, Diff(a,b)) == b. b2abea0 would not have been a wrong answer from a
library call under that scheme; it would have been silent corruption of stored deltas on
any keyed list whose tag changed. The coverage is a prerequisite for the lowering, and the
lowering is what removes the scope vocabulary concession (5hmq80f3h12krh1mbsn0).