# eliminate forged ir links completely

A forged link is a Parent/ParentField/ParentIndex triple that says a node is a child
where it is not: the parent's Fields or Values do not contain it. The link renders,
walks and answers Root() exactly as a real one does, and nothing checks it -- so the
only thing separating a forged link from a true one is whether anybody wrote it down.

The panic this issue was originally filed as is fixed (1029af7, c003bd5, verified: the
repro is clean and all four document shapes refuse instead of crashing). What is left is
the general form, which the first discussion note below raised and the fix answered by
implementation rather than by argument.

## Where the live one is

`absentAt` (patch.go:411), on the container branch:

    func absentAt(doc *ir.Node, field string, index int) *ir.Node {
        res := ir.Null()
        switch doc.Type {
        case ir.ObjectType, ir.ArrayType, ir.CommentType:
            res.Parent, res.ParentField, res.ParentIndex = doc, field, index
        default:
            res.Parent, res.ParentField, res.ParentIndex = doc.Parent, doc.ParentField, doc.ParentIndex
        }
        return res
    }

The placeholder stands for a field the document does NOT have -- that is what it is for --
and is then linked as the child at that field. `Patch({}, {a: !rename [...]})` produces a
null claiming to be `{}`'s child at `a` while `{}` has no field `a`. Path() renders `$.a`
and Root() answers the document, both from a link that describes nothing in the tree.

Path()'s assertion does not catch this. It asks what KIND the parent is -- Object, Array
or Comment -- and never whether the parent contains the child, which is precisely why the
forged link passes and the scalar one did not.

The scalar branch is a forged link already eliminated: 1029af7 stopped claiming a place at
a field of a number, which was an IMPOSSIBLE claim rather than merely an untrue one, and
so was the one Path() noticed.

## Assessment: absentAt needs a re-design, not a fix

The link cannot simply be deleted. It is load-bearing, and 031eb50 added it deliberately
to fix a silent wrong answer:

  - a placeholder with a nil Parent is a node in no tree, indistinguishable from a
    document root, since both answer nil to Parent
  - an operator asking which document it is in has nothing else to ask: OpContext carries
    DefEnv, EvalOpts, SchemaRegistry and Config, and no document
  - so `!get-path(root)` anchored at the placeholder rather than the document and errored,
    and `!list-path(root)` answered the EMPTY LIST, silently

Two tests pin the current behaviour and would have to be rewritten, not merely updated:
`TestAbsentPlaceholderKnowsItsPlace` (absent_place_test.go) asserts Parent, ParentField,
ParentIndex and Root() on the placeholder, and `TestTheAbsentPlaceholderStandsWhereAScalarStands`
(absent_scalar_test.go) asserts the scalar branch's inherited linkage.

So Parent is carrying two meanings at once -- "I am the child stored here" and "this is
the document I belong to" -- and only the first is what a link is. Eliminating the forgery
means giving the second meaning its own carrier.

### The shape it would take

Put the anchor in OpContext, which is already threaded through every operation and cloned
where isolation is needed. Set it once where a patch begins; have the three consumers of
`doc.Root()` in the patch path read it instead -- get_path.go:168 (`anchor = doc.Root()`)
and script_funcs.go:19 and :28 (getpath/listpath). Then absentAt has nothing to forge and
can leave Parent nil.

Care is needed on two points before this is written:

  - a nested or sub-document patch must not inherit an outer anchor, so whatever sets it
    has to be the same boundary Patch already treats as "the document"
  - error messages lose `$.a` and fall back to the document's own path, which is less
    useful; if that matters, the place should be passed to the operator explicitly rather
    than smuggled through a link

Four call sites build placeholders: patch.go:178, :306, :326, :493.

## Possibly in scope, NOT reproduced

`.Comment` is a side channel, and its node is given `Parent: target` where target may be
any type -- mergeop/comment.go:349, gomap/to.go:569, and emitted into every generated
codec by gomap/codegen/generator.go:2959. There "parent" means "the node I annotate", not
"the container I am in", which is the same overloading of the same field.

Recorded as scope, not as a defect: no production caller reaches Path() or Root() on a
`.Comment` node, and the panic was only observed on a node built by hand in a probe. It is
listed here so a sweep for forged links does not stop at absentAt.

## Provenance

Filed as the Path() panic (see the discussion below, where the diagnosis is corrected --
the assertion was right and blaming it was the mistake). The general question was raised
there and left undecided:

    Either that is a legitimate "where this would go" device and the assertion is only
    about the parent's KIND, or absentAt should not be forging links at all.

This issue is that question, reopened as the work.
