# ir/mergeop: !ir's view is neither a value nor a place, and the IR representation is defined twice

`!ir` matches a pattern against a node's IR representation, and it does that by building one:

    func (i irOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
        return f(IRView(doc), i.child, ctx)
    }

The view is a shim so the generic object matcher can do the work -- a Number node has no
fields, and `!ir {int: 3}` is an object pattern, so something object-shaped has to exist
for `f` to walk. Everything below follows from that one shim.

## The shape is neither a value nor a place

IRView is parentless at the top, by intent and by documentation:

    // The view has no parent, so it is not a place in the document and an
    // explanation reports it at the node !ir was applied to -- the convention
    // !field and !tag follow for the nodes they synthesize.

but underneath it is document-attached: `fields` and `values` are

    put("values", &ir.Node{Type: ir.ArrayType, Values: doc.Values})

so those array wrappers are the only nodes in the tree whose children do not point back
at them -- the elements' Parent still points at `doc`. Half-attached: parentless on top,
in the document below.

That deviates from an invariant the library otherwise keeps deliberately. `ir.Node.Root()`
is a walk to nil. `eval/expand_env.go` sets `repl.Parent = node.Parent` on substitution
rather than letting a splice stand. The convention is coherent roots and no splicing, and
the view is the exception.

## A parentless node already means something else

`Parent == nil` says "document root". The view uses it to say "not in a document". Those
are two different facts on one signal, and nothing downstream can tell them apart --
`Root()` answers the view, indistinguishably from answering a real root.

Explain pays for it. `pathOf` finds the root by node IDENTITY:

    for p := n; p != nil; p = p.Parent {
        if p == e.root {

and carries a `rooted bool` and a `lastPath` fallback for the nodes that never reach it.
The sharing of `doc.Values` rather than copying is what keeps that identity walk working
for anything under `fields`/`values` -- so the half-attached shape is not a design, it is
Explain's identity coupling showing through the operator.

It also decides an unrelated question. In designing `!get-path(root)` (nxybjwvch12ksbj8hxn0
thread), `$root` is `doc.Root()` and needs nothing threaded through OpContext -- BECAUSE
the invariant holds. Under `!ir` it answers the view, which is defensible only while the
exception stays unnamed.

## The IR representation is defined twice, and they already disagree

IRView's `put()` calls are one definition. `_ir` in schema/base.tony is another:

    _ir:
      type: !or [ Comment Null Bool Number String Array Object ]
      fields: .[array(field)]
      values: .[array(ir)]
      string: .[string]
      int: .[int]
      float: .[float]
      number: .[string]
      bool: .[bool]
      lines: .[array(string)]

Two disagreements, today, unchecked:

  - IRView puts `tag` and `comment`. `_ir` declares neither -- while `!ir`'s own doc
    comment advertises the first: `!k v` is `!ir {type: String, tag: "!k", string: "v"}`.

  - `_ir` says `values: .[array(ir)]` -- RECURSIVE, every value is itself an ir, which is
    a full dump. IRView is one level deep and puts the document's own nodes there, and
    documents that as the design: "a node whose values are nodes has a view whose values
    are those nodes, not views of them. Writing !ir again asks for the next level."

So the schema describes the full dump and the operator implements a one-level shim. A
pattern written from either one is wrong against the other, and nothing says so. This is
the same class the docs table already guards with a test.

## Three coherent designs, and today's is none of them

1. mergeop does the dispatch in code. `!ir` reads its child's fields against `doc`'s
   struct fields itself, recursing into `f` only for the sub-patterns under `fields:`/
   `values:` with the document's own nodes. Nothing synthesized, invariant intact, and
   the absence rule ("unset is absent, not null") becomes explicit rather than emergent
   from which `put()` calls fire.
   Costs: the view is no longer an object, so the operator vocabulary stops applying AT
   it -- `!ir !has-path int`, `!ir {values: !all ...}` -- unless reimplemented.

2. A full dump: materialise the whole IR-as-document, recursively, and match it as an
   ordinary document. Self-consistent, properly parented, one definition, and `_ir`
   already describes exactly this.
   Costs: a shallow question about one node's `int` materialises the whole subtree. Note
   the counterweight -- with a full dump you write `!ir` ONCE instead of at each level,
   so the repeated-shim cost goes away with it. Explanation changes character: nothing
   in the dump is a document node, so everything under `!ir` is reported at the `!ir`,
   which is more consistent but loses the real document path for a deep failure.
   And it is a breaking change to every nested `!ir` pattern, since `values` would hold
   views rather than nodes.

3. Keep a shim, but canonicalise it: a `Dump` (full) and a `DumpOne` (one level) sharing
   one definition, so the representation is stated once. If DumpOne returns a shim TYPE
   rather than an *ir.Node then MatchFunc cannot take it, which forces (1); if it returns
   an *ir.Node then the exception must be NAMED rather than signalled by a nil Parent.

Whichever way it goes, two things want doing on their own: one definition of the IR
representation shared by `_ir` and the operator, with a test that they agree; and Explain
finding its root by something other than node identity, since that coupling is what fixes
the operator's shape from the outside.

Filed rather than fixed: it is a design choice about what `!ir` IS, and (2) is a breaking
change to nested patterns.