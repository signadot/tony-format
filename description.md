# patch: a head-commented element introduced into an absent list loses its tag — a rule's `!not.or` lands as a bare string

## Symptom

An array element that carries a head comment loses its tag when the patch introduces the list (the document has no list at that path, or the element is past the end of the one it has):

```
tony.Patch({}, {list: [a, # a note\n !t x]})   ->  {list: [a, x]}      expected {list: [a, !t x]}
tony.Patch({}, {list: !insert.raw [a, # a note\n !glob x]})  ->  {list: [a, x]}   expected {list: [a, !glob x]}
tony.Patch({}, {list: !insert.raw [a, !glob x]})  ->  {list: [a, !glob x]}   (no comment: fine)
tony.Patch({}, {list: [!t x]})                    ->  {list: [!t x]}          (no comment: fine)
```

Comments on, as logd applies a write (`mergeop.Comments(true)`), the inner value keeps its tag, so the document logd holds is right and a read answers it. Comments off, the default and what a client watch uses to level its own copy from the commit's patch, the tag is gone: the same commit reads one way through a Get and another through a watch.

## Source

`go-tony/patch.go`, the ArrayType case of `doPatchWith`, in both places an element is applied against absence (`doc.Type != ir.ArrayType`, and the past-the-end loop):

```go
if _, op, _, _, _ := mergeop.SplitChild(elt); op == "" {
    yy.Tag = elt.Tag
}
```

`elt` is the comment WRAPPER when the element has a head comment: its tag is "", `SplitChild` sees no op, and the assignment overwrites the tag `patchAndAnswer` answered on the value with the wrapper's empty one. The assignment is there to keep the element's own presentation through a merge into absence (`mergedTag` strips it); the element's own tag is the one on the value the comment stands for.

Proposed: split and copy from the node the comment stands for, onto the node the result stands for (with comments kept the result is a wrapper too):

```go
inner := ir.Uncomment(elt)
if _, op, _, _, _ := mergeop.SplitChild(inner); op == "" {
    ir.Uncomment(yy).Tag = inner.Tag
}
```

in both loops. With comments off `yy` is the value and this is the same assignment as before for an uncommented element; a commented `!delete null` element past the end still answers nil before the assignment is reached.

Context: found while making verse's trigger engine follow its charter records through one watch (verse wv2pp5j0h12ks1ncwnn0, step 2). deploy-announce.tony's rule has `- # …\n !not.or .[seen]` under `!and`; the record logd holds is right, the engine's copy decoded from the watch's picture of the commit has `.[seen]` with no `!not.or`, and the rule announces every tag rather than the new ones.