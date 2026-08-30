# Scoped tag resolution, and contexts that resolve a prefix

A dev plan for the two steps after namespaced registration.

Registration is already namespaced: `mergeop.RegisterNamespaced("acme", sym)` makes
`!acme:shout`, `Register` refuses a name holding the separator, and no built-in is named
with one (`4a79826`). That closes the collision between a consumer's tags and a future
release's.

It does not close anything else. This plan is the two steps that do:

- **C — resolution is per context**, so two consumers in one process cannot reach each
  other's operations, and a test that registers cannot leak into the next one.
- **D — a context resolves a prefix**, so a document says which namespace it means, the
  identity of that namespace is a URI rather than a spelling, and `o` keeps the built-ins
  unprefixed because they are the default context.

D needs C. Resolution has to become context-aware before a context can do any resolving.

## Where resolution happens today

One process-global map in `mergeop`, reached by `Lookup`. What matters for the plan is
that the question "is this tag an operation?" is asked in eight places, and only two of
them are the dispatch:

| site | asks | has an OpContext |
|---|---|---|
| `mergeop/split_child.go:21` | the resolution walk itself | no |
| `patch.go:109` | dispatch | **yes**, and uses it two lines later |
| `match.go:114` | dispatch | **yes** |
| `mergeop/addtag.go` `chainHasOp` | does an operation trail this one | no |
| `mergeop/register.go` `libdiff.IsOp` | should a diff escape this | no, and it is a mutable package var |
| `system/logd/storage/read_subtree.go` `hasOperator` | can a patch be re-rooted here | no |
| `system/logd/api/lowering.go` `firstRelativeOp` | may the log keep this | no |
| `system/docd/server/split.go` | is this routed or applied | no |

Five of those are separate implementations of one predicate. That is the risk this plan
carries, and it is not hypothetical: three defects fixed in the last week were two places
reading one tag chain differently (`2w62pyyah12ksqh0jdn0`, `1hf5pzj6h12ksd40jdn0`,
`fch8ptynh12ksfvvjdn0`). Making resolution scoped multiplies the ways they can disagree,
so the plan unifies them before it scopes them, not after.

`Diff` is the awkward one: it takes no context at all, and asks through
`libdiff.IsOp`, a package-level `var` that `mergeop`'s init assigns and anything could
reassign.

## C — resolution is per context

**C1. A registry type, and the global as one instance.** Extract the map into a
`mergeop.Registry` with `Lookup`, `Register`, `RegisterNamespaced`, `Symbols`. The
package-level functions keep working, against a package-level instance. Pure refactor:
no caller changes, no behaviour changes, and the existing suite is the check.

**C2. One predicate, one place.** Replace the five hand-written "is this an operation"
walks with one exported function over a registry. Each call site keeps its own meaning --
`firstRelativeOp` still asks about storability, `hasOperator` still asks about re-rooting
-- but they stop each having their own idea of what an operation is. Do this while
resolution is still global, so the change is provable by the existing tests rather than
tangled with the next step.

**C3. `SplitChild` takes a registry.** This is the only signature that has to change, and
it is public with ten call sites: two inside `mergeop`, five in `patch.go` and `match.go`,
and three in logd (`server/session.go`, `storage/lower.go`, `storage/tx/array_write.go`). Add `SplitChildIn(reg,
node)` and leave `SplitChild` delegating to the built-ins, so call sites migrate one at a
time and no consumer breaks.

**C4. A registry on `OpContext`, nil meaning the built-ins.** `ctx.Lookup(name)` consults
the context's registry, then the built-ins. Dispatch at `patch.go:109` and
`match.go:114` becomes `ctx.Lookup(tag)` -- both already hold `ctx`.

**C5. Diff carries one too.** A `DiffOpt` supplying the registry, threaded into `libdiff`
as a parameter rather than through the package var. The var goes; nothing should be able
to change what counts as an operation by assignment.

**C6. A `Tool` owns a registry.** `tony.Tool` is where a consumer's world already lives
(it holds `Env`). Registering on a Tool rather than on the package is the API that makes
two consumers in one process independent, and makes a test's registrations local to it.

What C is worth, stated so it can be checked: two `Tool`s with the same prefix bound to
different operations both work, and neither sees the other's.

## D — a context resolves a prefix

The data model exists and is unused for this. `schema.Context` carries a `URI`, a
`ShortName`, `OutIn`/`InOut` mapping short names to URIs both ways, `Tags`, and
`Extends`; `ContextRegistry` holds them; `FromIR` already parses the JSON-LD shape
`{"acme": "tony-format/context/acme"}`. What is missing is that nothing binds a context to
the symbols that implement it, and nothing consults a context when resolving a tag.

**D1. A context names a registry.** Bind context URI -> `mergeop.Registry`. The URI is the
identity of a namespace; the prefix is only how a document spells it.

**D2. A document's bindings reach the OpContext.** Decide where the declaration lives --
`FromIR` reads the mapping, but nothing today carries it from a parsed document to the
`OpContext` that patches it. This is the step with a real design choice in it, and it
should be settled before D1 is written.

**D3. Resolution follows the prefix.** `acme:thing` -> prefix `acme` -> the document's
bindings -> URI -> registry -> symbol. The built-ins are the default context, always
bound, which is exactly why `o` keeps them unprefixed and why nothing else is.

**D4. An unbound prefix is an error.** Not a miss, not a fallback to the global. A
document holding `!acme:thing` in a process which has never heard of `acme` must say so;
silently treating it as data is how a patch stops meaning what it said.

What D is worth: two consumers may both use the prefix `ext` in their own documents
without meaning the same operation, because the URI decides and the prefix is local.

## Decisions to settle before writing D

- **Where a document's context bindings live.** A field? A separate argument to
  `PatchWith`? Both, with the field being the portable one? D2 is blocked on this.
- **What logd stores.** `api.ValidateForStorage` asks `mergeop.Lookup` whether a tag is
  storable. A stored patch holding `!acme:thing` is only meaningful to a reader who can
  resolve `acme`, so either the binding is stored with it or the store refuses it. This
  decides whether namespaced operations are usable in logd at all, and it is worth
  answering early because it may pull D forward.
- **Whether `Register` stays.** After C6 the package-level registry is a convenience,
  and it is also the thing that makes tests leak. It could become the built-ins only,
  with consumers required to hold a Tool.

## Risks

The five predicates are the main one, addressed by doing C2 first: while resolution is
global they cannot disagree about scope, so unifying them is testable on its own.

The second is that C changes nothing observable if it is done well, which makes it hard
to know it worked. The check is C's stated worth above -- two Tools, same prefix,
different operations -- written as a test before C1 rather than after C6.
