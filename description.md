# tony: Patch shares the input's untouched subtrees and re-parents them to the result, so the input no longer walks back to itself

Found by the go-tony docs sweep (3d7t1khxh12krsddmdn0). Reproduced at 546bd53.

## What happens

For every field a patch does not touch, the result holds the input document's own
node, not a copy: objMergeFast (patch.go:339, :360), and the map path the same way
(patch.go:507, then ir.FromMap). Then every result value is re-parented to the
result (patch.go:390-392; ir/node.go:158 on the map path). A node has one Parent, so
a node in two trees can name only one: afterwards the input still lists the node,
and the node names the result -- Parent, ParentIndex and ParentField.

    doc = {a: 1, b: {c: 2}};  res = Patch(doc, {a: 3})
      res.Values[1] == doc.Values[1]           // shared
      b.Parent == res, b.Root() == res         // doc's own child names res
    Patch({b: 1, c: 2}, {a: 0})                // doc's b now says ParentIndex 1

A wrong answer, with no error:

    doc  = {spec: {v: 1}, status: {}}
    res1 = Patch(doc, {spec: {v: 10}})
    res2 = Patch(doc, {spec: {v: 20}})         // status is shared by all three, names res2
    Patch(res1, {status: {y: !get-path(root) spec.v}})          -> y: 20 (read from res2)
    Patch(res1.Clone(), {status: {y: !get-path(root) spec.v}})  -> y: 10

The patch's own nodes are never shared into the result (nine shapes checked).

## What it breaks

The parent-tree invariant (ir/doc.go, Parent Links): walk down from doc to b and back
up, and you are at res. And mergeop/field.go:64's own contract: "a patch never mutates
what it is given, and a store that keeps a document and steps it by each patch relies
on that." Root(), KPath(), !get-path(root) and eval's getpath()/whereami() answer from
the other document, silently.

## Who it reaches

No production caller in go-tony today: the storage fold and lowering, watch stepping
(seedAt reads its own base), dirbuild, o patch and o eval all drop their input or only
Diff it, and Diff reads no links. The keeper field.go names, the stepped head, went in
31d3e27. It reaches a library caller -- verse included -- that keeps a document after
patching it, or patches one document twice.

## Decided: (a) the input stays coherent

Patch copies what it would share, so input and result each walk back to themselves.
The alternative, stating that Patch hands the input's subtrees to the result, was
declined. The cost is the point of objMergeFast: 557us -> 149us on a 3000-field object
(v552mdbqh12kr7dtgdn0) and 270us -> 53us for sharing key nodes (rkb7p8v5h12ksdnmgsn0);
a copy makes each patch cost the document, and every watcher step pays it. Measure the
fold and watch paths at those sizes before and after. The result's shared key nodes
(patch.go:372-376, which name doc by design) fall under the same rule.

Related: kbkxf53ph12krswpj9n0 (forged links), the neighbouring shape: here a node is
contained twice, there a link names a parent that does not contain it.