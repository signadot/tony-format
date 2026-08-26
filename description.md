# match/patch: !get-path and !get-paths, so a pattern can say 'the value over there'

There is no way to say "the value that is over there" in a pattern or a patch.

`!at(kpath)` walks to a path and applies a MATCH there; `!embed` hands the whole doc node
to an operand with no path; `!has-path` answers whether. None of them ANSWERS WITH a value
at a path, so nothing can be compared against one, written from one, or bound to a name.

## Where it came from

!let bindings cannot reference the input document, and not for want of syntax: Instance
builds `bindings` from the pattern's `let:` list and never sees a document, so a binding
value is IR taken literally.

    doc {a: 1, b: 0}   !let {let: [{v: $.a}], in: {b: .[v]}}   ->  {a: 1, b: $.a}

The document reaches the body only when the expanded result is applied, which does give an
indirect route -- a binding may name an OPERATION, and the operation reads the document
wherever the body puts it:

    doc {a: 1}   !let {let: [{w: !embed(HERE) {was: HERE}}], in: {a: .[w]}}   ->  {a: {was: 1}}

!embed is doing the reading there, at `a`, because that is where `.[w]` placed it. The
binding is still a literal node. What is missing is the value itself.

## The operation

Two, because the path language already has the distinction and enforces it elsewhere:
`kpath.HasWild()` separates a path which names one node from one which names a set, and
`..` is documented as a query segment which "a stored path cannot hold, and the things
which keep stored paths refuse rather than pretend".

    !get-path <kpath>     the node at the path.  A wild path is refused at Instance,
                          where !at already refuses one it cannot parse.
    !get-paths <kpath>    the nodes at the path, as a list.  Any path; a path naming one
                          node gives a list of one, so there is no special case.

The path goes in the OPERAND, as !has-path takes it, rather than in a tag arg as
!at(kpath) does: here the path is the value the operation is about, not a modifier on one,
and it keeps a long path readable.

`ir.Node.GetKPath` and `ir.Node.ListKPath` are the two implementations, and `kpath.Parse`
at Instance is the check.

## The anchor

kpath is relative to the node it is resolved against, which is what the operation wants:
the node this op was applied to, the same one `f`/`pf` are handed in the recursive call.
Absolute addressing goes in a tag arg rather than in the path:

    !get-path spec.image          relative to the node the op meets -- the default
    !get-path(root) spec.image    anchored at the document

NOT a sigil inside the path. kpath spells the root as the EMPTY path, and cmd/o/querypath.go
took the `$` out of the query surface deliberately -- "that '$' carries no information --
every path has it" -- so a root sigil in the grammar re-litigates that decision and reaches
every stored path, the index, watch names and error text.

`root` is `doc.Root()`, a walk to nil, and needs nothing threaded through OpContext. That
holds because the library keeps the invariant on purpose: Root() is the blessed primitive,
eval re-parents on substitution rather than let a splice stand, and a node genuinely outside
a document is made parentless ON PURPOSE. As of 264837c the one node where that signal was
overloaded -- !ir's view -- is gone, so Root() no longer answers "a view" where it looks
like it is answering "a document".

## Examples

    # patch: one value copied from elsewhere in the document
    spec:
      template:
        spec:
          containers:
            sidecar: {image: !get-path(root) spec.template.spec.containers.main.image}

    # match: a relation between two parts of one document, which no pattern can say today
    status: {replicas: !get-path(root) spec.replicas}

    # a name bound to what the document holds -- the case this started from
    !let
      let:
      - img: !get-path(root) spec.image
      in:
        containers:
          main: {image: .[img]}
          sidecar: {image: .[img]}

    # per-element, which is where a binding earns the most
    !all.let {let: [{n: !get-path metadata.name}], in: {labels: {app: .[n]}}}

    # the plural, over a wild path
    !get-paths containers[*].image

## Bindings which read the document

Instance already stores bindings as raw *ir.Node and only buildEnv is eager, and both
Match and Patch already hold the node the let was applied to. So the delta is expandBody
taking the doc and resolving each binding value against it before building the env.

The rule to keep straight is that this adds a second environment on a DIFFERENT AXIS.
Names come from the lexically enclosing let, outward-in, which is what expandScoped does.
The document node comes from the let's own position. Nesting keeps working only if those
stay two passes: expandScoped rewrites `.[name]` and rebuilds containers, so a !get-path
node rides through the outer pass untouched and is resolved by the inner op at ITS
position. eval.ExpandIR must not learn about paths.

## Implementation questions

1. A PATH WHICH NAMES NOTHING. It must not become null. That is the trap this operator
   family has sprung three times already in !let -- an unbound body ref, an unbound
   binding, and a cycle -- and it is worse in a patch than in a match, since a null
   pattern merely matched everything while a null patch is WRITTEN.
   !at's precedent is "names nothing -> does not match", which reads correctly for a
   match and has no counterpart in a patch. So: an error, or an explicit default written
   at the call site? And is `!get-paths` naming nothing an empty list or the same refusal?

2. THE NODE OR A COPY OF IT. GetKPath answers the document's OWN node. For a match that is
   right and better -- it has a real position, so Explain reports a failure there rather
   than at the operator, which is exactly what !ir's cloned list gives up (264837c).
   For a PATCH the answer is installed in the result, and a node cannot belong to two
   trees: ir.FromSlice and the container builders re-parent what they are given. So the
   patch side has to clone on install, or the result splices a node out of the document.
   Which side clones, and does the match side get to keep identity while the patch side
   does not?

3. WHAT THE READS SEE. If the anchor is the input document, `{a: !get-path(root) b, b:
   !get-path(root) a}` is a swap rather than order-dependent. That is worth having as a
   stated guarantee, and it needs checking against what doPatchWith actually hands down
   as it descends -- the original child, or something already rebuilt.

4. THE ANCHOR VOCABULARY. `root` and the default; is there a third worth having (the
   enclosing !all element, say)? And what does `(root)` mean under an operator that
   changes the subject -- !field and !tag hand over synthesized scalars with no parent, so
   Root() there is the scalar itself, which is coherent but may want refusing.

5. STORAGE. A !get-path in a stored delta re-evaluates against a base that has moved, so it
   is not storable. logd's storableTags is an allowlist, so it is excluded by doing
   nothing; it wants a whyNotStorable reason so the refusal says why. Diff never emits it.

6. WHAT IT COSTS THE MATCH SIDE. A pattern holding one is no longer readable independently
   of the document it is matched against. logd already prices that in for !let --
   whyNotStorable says "it is conditional on the document it meets" -- so the constraint is
   consistent, but it is a real change in what a pattern IS and should be said out loud in
   matchpatch.md.

Related: nxybjwvch12ksbj8hxn0 (the let work this came out of), p4tzbzx7h12kr6tkhxn0 (the
Root() signal this relies on being unambiguous).