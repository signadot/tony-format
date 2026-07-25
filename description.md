# tony-codegen/gomap: hand-written ToTonyIR codecs are unreachable or unusable in several common shapes (tracking)

Tracking issue for defects found while migrating [verse](https://github.com/signadot/verse)
from `encoding/json` to tony as its default representation. All against **v0.0.87**.

They share a theme: a type with a **hand-written** `ToTonyIR`/`FromTonyIR` is either not
reached, or cannot coexist with `tony-codegen` in the same package. Individually each has a
workaround; together they force an all-or-nothing choice per package, which is what makes it
worth filing as one issue.

Each item below states what verse does to work around it, so the workarounds can be removed as
they are fixed. **I will add to this as I find more.**

---

## 1. `gomap` reflection dispatches to `ToTonyIR` only for POINTER values

`toIRReflectValue` (`gomap/to.go`) checks for the method *inside* its pointer branch:

```go
if kind == reflect.Ptr {
    if val.IsNil() { return ir.Null(), nil }
    // Check for ToTonyIR() method on the value type (works for both value and pointer types)
    if method := val.MethodByName("ToTonyIR"); method.IsValid() {
        return callToTonyIR(method, opts...)
    }
    ...
}
```

The comment says "works for both value and pointer types", but a value never reaches it — the
struct/map branches below walk the field structurally instead.

**Why it matters:** it fails *silently*, producing plausible-looking output. A type whose whole
purpose is a custom wire form is quietly rendered as its raw Go shape.

Note the asymmetry with `encoding.TextMarshaler`, which *is* checked for non-pointers a few
lines further down (`// Check for encoding.TextMarshaler for non-pointers`), including an
addressable-pointer-receiver retry. `ToTonyIR` gets neither.

```go
type Leaf struct{ V string }
func (l Leaf) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) { return ir.FromString("LEAF"), nil }

type Host struct {
    ValLeaf Leaf  `tony:"field=valLeaf"`
    PtrLeaf *Leaf `tony:"field=ptrLeaf"`
}
// gomap.ToTony(&Host{PtrLeaf: &Leaf{}}) =>
//   ptrLeaf: LEAF        <- dispatched
//   valLeaf:
//     V: ""              <- NOT dispatched, walked structurally
```

**Suggested fix:** hoist the method check above the `reflect.Ptr` branch and mirror the
`TextMarshaler` treatment (try the value, then `val.Addr()` when addressable).

---

## 2. A named MAP type is never dispatched to, on either path

`type Match map[string]any` with a hand-written codec is ignored by **both** paths:

- reflection: it is a value, so item 1 applies;
- codegen: `case reflect.Map:` in `gomap/codegen/generator.go` inlines
  `for k, v := range s.Field { ... }` → `ir.FromMap(...)`. It never asks whether the *named map
  type itself* has a codec. Only `case reflect.Struct:` and pointer-to-struct emit
  `s.Field.ToTonyIR(...)`.

**Why it matters:** a named map is the natural Go spelling for an open-ended document fragment
that still needs custom decoding — which is exactly the kind of type that has a hand-written
codec. verse's `trigger.Match` accepts either an object or a `"system:kind:id"` string, and
validates its key set; both are lost.

**Workaround in verse:** none available for the map itself — the enclosing types are
hand-written so the map is never reached through generated or reflected code.

Related but distinct from cc5rbhv8h12krppwjhmg (`map[string]StructType` → invalid
`isZeroValue`), which is about the *value* type of a map rather than the map type itself.

---

## 3. An unannotated local type in a field generates SILENTLY NON-COMPILING code

If a field's named type has no `//tony:schemagen=`/`schema=` annotation, codegen cannot resolve
it and falls back to emitting integer conversions.

```go
type Leaf struct{ V string }     // deliberately not annotated
type NamedMap map[string]string  // deliberately not annotated

//tony:schemagen=spike-host,notag
type Host struct {
    ValLeaf Leaf     `tony:"field=valLeaf"`
    PtrLeaf *Leaf    `tony:"field=ptrLeaf"`
    ValMap  NamedMap `tony:"field=valMap"`
}
```

`tony-codegen -dir .` exits 0 and writes:

```go
irMap["valLeaf"] = ir.FromInt(int64(s.ValLeaf))
if s.PtrLeaf != nil { irMap["ptrLeaf"] = ir.FromInt(int64(*s.PtrLeaf)) }
irMap["valMap"] = ir.FromInt(int64(s.ValMap))
```

```
cannot convert s.ValLeaf (variable of struct type Leaf) to type int64
cannot convert *s.PtrLeaf (variable of struct type Leaf) to type int64
cannot convert s.ValMap (variable of map type NamedMap) to type int64
...and 3 more in FromTonyIR
```

**Why it matters:** the failure mode is an unresolved type being treated as an integer. An
error ("cannot resolve type Leaf for field ValLeaf — annotate it or mark the field `omit`")
would be strictly better than emitting code that cannot build. Same family as
cc5rbhv8h12krppwjhmg — bad type resolution surfacing as invalid Go rather than a diagnostic.

---

## 4. Annotating a type to make it resolvable also generates its methods — colliding with
hand-written ones

This is the bind that items 2 and 3 create together, and the reason this is one issue:

- **Don't annotate** the hand-written type → the referencing type generates invalid code (item 3).
- **Do annotate** it → codegen writes `ToTonyIR`/`FromTonyIR`/`ToTony`/`FromTony` for it, which
  collide with the hand-written methods in the same package (`method redeclared`).
- **`schema=NAME` (usage mode)** does not help: it makes the type resolvable — the caller
  correctly emits `s.ValLeaf.ToTonyIR()` — but it *still generates all four methods*.

So there is no way to say "this type has its own codec; just call it." A package must be
entirely generated or entirely hand-written.

**Workaround in verse:** the whole charter grammar package (`trigger` — 8 types) is hand-written,
including 4 types whose mapping is purely mechanical and would otherwise be generated.

**Suggested fix:** a marker that means *resolvable, but do not generate* — e.g.
`//tony:codec=custom`, or having `schema=NAME` skip method generation (which would match what
"an existing schema is the source of truth" already implies), or simply skipping generation for
any type that already declares the methods.

---

## 5. Generated code drops `opts...` on nested field calls

Generated nested calls are bare, so a nested codec cannot see the encode options — including the
**target format**:

```go
// system/logd/api/api_gen.go
node, err = s.Match.ToTonyIR()      // nested field: no opts
...
node, err := s.ToTonyIR(opts...)    // self-call in the ToTony wrapper: opts passed
```

Root cause is `methodAcceptsOpts` in `gomap/codegen/generator.go`:

```go
// Types in the current package being generated will have opts
if baseType.PkgPath() == currentPkgPath { return true }
// For external types, use reflection to check
pt := t; if t.Kind() != reflect.Ptr { pt = reflect.PtrTo(t) }
method, ok := pt.MethodByName(methodName)
if !ok { return false }
```

Neither branch fires for types the parser resolved from source: `PkgPath()` on the synthetic
`reflect.Type` does not match `currentPkgPath`, and `MethodByName` on a synthetic type finds
nothing. It does not converge on a second run either — I regenerated and diffed, output
identical. Every nested call in this repo's own generated files is bare.

**Why it matters:** JSON cannot represent tags at all, and `encode.Encode` correctly errors
rather than dropping them (`cannot encode tags in json`). A type that emits a tagged node
therefore needs to know when the target is JSON so it can emit an alternative spelling — and
with opts dropped it can only know that when it happens to be the top-level value.

**Workaround in verse:** format-awareness was moved out of the types entirely, into a single
`Detag` pass over the finished IR that rewrites `!tag value` to `{tag: value}` when the target
is JSON. This turned out better than the original design, so this one is lower priority for us
— but the dropped options are surprising, and anything else that legitimately needs the encode
options (comments, colors, wire mode) has no way to get them.

---

## 6. Papercut: empty input is `(nil, nil)`, but whitespace-only input is a parse error

```go
parse.Parse(nil, parse.ParseTony())          // (nil, nil)   <- nil node AND nil error
parse.Parse([]byte("   "), ParseTony())      // error: imbalanced document: extraneous indent
parse.Parse([]byte("\n\t\n"), ParseTony())   // (nil, nil)
```

Two separate papercuts:

- `(nil, nil)` reads as success, so a caller that does not explicitly check for a nil node will
  dereference it. An explicit `ErrEmptyDocument` (or a null node) would be harder to get wrong.
- Whether blank input errors depends on *which* whitespace it is. A body of spaces is a
  perfectly ordinary thing to receive over HTTP, and "extraneous indent" is a confusing thing to
  say about it.

**Workaround in verse:** `codec.ParseNode` trims first and returns a null node for anything
blank; `codec.ParseNodeOrNil` keeps the "no document" distinction for config layering.

---

## Repro environment

go-tony v0.0.87, Go 1.25.1, darwin/arm64. Items 1, 2, 5 and 6 are pinned as regression tests in
verse's `codec/upstream_test.go`, including one that reads this repo's generated
`system/logd/api/api_gen.go` and fails if nested calls ever start passing `opts...` — so verse
will notice when these are fixed.