# tony: scoped tag resolution, and contexts that resolve a prefix (C and D)

Plan: `go-tony/docs/tag-namespacing-plan.md` (559a83f).

Namespaced registration landed in 4a79826: `RegisterNamespaced("acme", sym)` gives
`!acme:shout`, `Register` refuses a namespaced name, and no built-in holds the
separator. That closes the collision between a consumer's tags and a future
release's, and closes nothing else.

Two steps remain, agreed as the direction:

C -- resolution is per context, so two consumers in one process cannot reach each
other's operations, and a test that registers cannot leak into the next one. It
already leaks: the summary-coverage test in mergeop had to be narrowed to built-ins
because the namespace tests register into the same global table.

D -- a context resolves a prefix, so a document says which namespace it means, the
identity is a URI rather than a spelling, and `o` keeps the built-ins unprefixed
because they are the default context. schema.Context already models all of that
(URI, ShortName, OutIn/InOut, Tags, Extends) and nothing consults it when resolving
a tag.

D needs C.

## The part worth knowing before starting

"Is this tag an operation?" is asked in EIGHT places, and five are separate
hand-written walks:

  mergeop/addtag.go            chainHasOp
  mergeop/register.go          the libdiff.IsOp hook
  system/logd/storage/read_subtree.go   hasOperator
  system/logd/api/lowering.go  firstRelativeOp
  system/docd/server/split.go

Three defects fixed in the last week were two places reading one tag chain
differently -- 2w62pyyah12ksqh0jdn0, 1hf5pzj6h12ksd40jdn0, fch8ptynh12ksfvvjdn0.
Scoping resolution without first unifying that predicate multiplies exactly that
failure, so the plan unifies it while resolution is still global and therefore
provable by the existing suite.

Diff is the other seam: it takes no OpContext at all and asks through
`libdiff.IsOp`, a package-level var that anything can reassign.

`SplitChild` is the only signature that must change: ten call sites, two in
mergeop, five in patch.go/match.go, three in logd. Adding `SplitChildIn(reg, node)`
lets them migrate one at a time without breaking a consumer.

## Decisions the plan does not make

- Where a document's context bindings live. `schema.FromIR` parses the JSON-LD shape
  already, but nothing carries it from a parsed document to the OpContext that
  patches it. D2 is blocked on this.
- What logd stores when a patch holds a namespaced operation.
  `api.ValidateForStorage` asks `mergeop.Lookup` whether a tag is storable, and a
  stored patch is only meaningful to a reader who can resolve its prefix -- so either
  the binding is stored beside it or the store refuses it. This may pull D forward.

## How C is checked

Two Tools, the same prefix bound to different operations, both working and neither
seeing the other's. Written before C1 rather than after C6, because C is otherwise a
refactor with nothing observable to show for it.