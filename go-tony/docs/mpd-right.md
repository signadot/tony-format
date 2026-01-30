# Structured Matching, Patching, and Diffing Done Right with Tony Format

If you work with YAML or JSON configuration---Kubernetes manifests, Helm
charts, IaC definitions, anything really in software ---you've probably hit the
same wall. You start with a nice declarative format. Then you need to diff two
versions, so you reach for `diff`. You need to patch a field, so you reach for
`yq` or `jq`. You need to match documents in a stream, so you reach for `grep`.
Before long, your "declarative" pipeline is held together by text munging.

The higher-level tools don't escape this either. Helm templates YAML as
strings. You can't diff two YAML documents as YAML---you convert to JSON
first. Config-as-data systems like kpt and porch target narrow use cases---
package management, WYSIWYG authoring---because they're built on top of a
hodgepodge data format model. They hide the mess rather than fix it. And the
mess leaks: sooner or later you're back to `grep`, `sed`, and `yq` to glue
things together.

But this article isn't about building a better Helm or kpt. It's about getting
the underlying data model right.  What happens when matching, patching, and diffing
all operate on the same typed tree representation, with the same extension
mechanism, producing output that feeds back into each other?

That's what Tony format does. It operates directly on the intermediate
representation (IR) of structured data---a typed tree of objects, arrays,
strings, numbers, booleans, and nulls. Matching, patching, and diffing are all
defined as operations on this tree, not on its serialized text. And because the
three operations share the same IR and the same tag-based extension mechanism,
they compose in ways that text-based tools never could, and they generalize in
ways config management tools never could either.

## The IR: One Tree to Rule Them All

At the core of Tony is `ir.Node`, a uniform representation for structured data:

```go
type Node struct {
    Type    Type      // Null, Bool, Number, String, Object, Array
    Fields  []*Node   // object keys
    Values  []*Node   // object values or array elements
    Tag     string    // operation tag, e.g. "!or", "!key(name)"
    String  string
    Bool    bool
    Int64   *int64
    Float64 *float64
}
```

This IR can be parsed from YAML, JSON, or Tony's own format, and encoded back
to any of them. Every operation---match, patch, diff---works on `*ir.Node`. No
serialization boundaries, no format-specific quirks. A patch written against
YAML works identically against JSON.

The `Tag` field is what makes things interesting. Tags are YAML-compatible
annotations (e.g. `!or`, `!dive`, `!key(name)`) that extend nodes with
operational semantics. They compose via dot-separation: `!all.field.glob`
chains three operations into one. The same tag mechanism drives matching,
patching, and diffing, which means the three operations speak the same
language.

Every tag resolves to a registered `Symbol` that implements the `Op` interface:

```go
type Op interface {
    Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error)
    Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, df DiffFunc) (*ir.Node, error)
}
```

Both methods are on the same interface. A single operation can define both
matching and patching behavior---`!if` tests a condition and applies a
transformation in one step. The `MatchFunc` and `PatchFunc` callbacks enable
recursive delegation back to the engine, threading context through the entire
tree. New operations can be added without modifying the core engine: just
register a new symbol.

## The Three Operations

### Matching

Tony's `Match(doc, pattern)` answers a simple question: does this document
satisfy this pattern? But "pattern" means something much richer than string
equality.

A plain match is structural. For objects, every field in the pattern must
exist in the document with a matching value. For arrays, elements match
positionally. For scalars, values must be equal. Crucially, the pattern is a
*subset*---the document can have fields the pattern doesn't mention.

```yaml
# Document
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
  namespace: production

# Pattern - matches because all specified fields are present and equal
kind: Deployment
metadata:
  namespace: production
```

This is already more useful than `grep 'kind: Deployment'` because it
understands nesting. But tags unlock the real power:

```yaml
# Match any Deployment or StatefulSet in the monitoring namespace
kind: !or [Deployment, StatefulSet]
metadata:
  namespace: !glob 'monitor*'
```

`!or` matches if any alternative matches. `!glob` matches against a glob
pattern. `!and`, `!not`, `!has-path`, `!type`, `!field`, and `!subtree` round
out the vocabulary. `!subtree` searches the entire document depth-first.
`!let` binds variables before matching. Each callback delegates sub-matching
back to the engine, so compound operations compose naturally.

### Patching

Tony's `Patch(doc, patch)` transforms a document by merging a patch into it.
Without tags, this works like a structural merge patch:

- Objects merge field-by-field. Patch fields override document fields; document
  fields not in the patch are preserved.
- Arrays merge positionally up to the shorter length, then the patch's
  remaining elements are appended.
- Scalars replace.

```yaml
# Document
metadata:
  name: frontend
  labels:
    app: web
spec:
  replicas: 1

# Patch
spec:
  replicas: 3
  strategy:
    type: RollingUpdate

# Result
metadata:
  name: frontend
  labels:
    app: web
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
```

The patch only mentions what changes. Everything else is left alone. This is
the same mental model as JSON merge patch (RFC 7396), but extended to
arbitrary structured data with tag support.

Tags turn simple patches into a transformation language:

**`!delete`** removes a field:
```yaml
metadata:
  annotations:
    old-annotation: !delete null
```

**`!if`** conditionally applies patches:
```yaml
!if
  if:
    kind: Deployment
  then:
    spec:
      replicas: 3
  else:
    !pass null
```

**`!dive`** recursively searches the document tree, applying conditional
patches to every subtree. This is how you express transformations over
arbitrarily nested structures without knowing the depth ahead of time.

A real example: Kubernetes CRDs embed OpenAPI schemas that can be thousands of
lines deep. Stripping `description` fields from `podTemplate` subtrees to
reduce CRD size used to require a shell script. With `!dive`, it's a patch:

```yaml
# Match any CRD, then dive into its version schemas
- match:
    kind: CustomResourceDefinition
  patch:
    spec:
      versions: !dive
      - match:
          podTemplate: null
        patch:
          podTemplate:
            description: !delete null
            properties: !dive
            - match: !irtype {}
              patch:
                description: !delete null
```

`!dive` walks the tree bottom-up, applying each match/patch pair at every
node. The outer dive finds any `podTemplate` subtree anywhere in the version
schema. The inner dive then strips `description` from every object inside it.
No matter how deeply nested the schema is, the patch handles it.  The result:
you can ship CRDs with pod templates inside without repeating megabytes of
repeated descriptions.

**`!all`** applies a patch to every element of an array or every value of an
object. **`!pipe`** shells out to an external command (marked unsafe, can opt
out).

#### Keyed Lists

One of the most practical features is `!key(field)`, which tells Tony to
treat an array as a map keyed by a field value. This solves the classic problem
of patching Kubernetes arrays like `containers` or `volumes`:

```yaml
# Document
spec:
  containers: !key(name)
  - name: app
    image: myapp:v1
  - name: sidecar
    image: proxy:v1

# Patch
spec:
  containers: !key(name)
  - name: app
    image: myapp:v2
```

Without `!key`, array patching is positional---swap the order and your patch
breaks. With `!key(name)`, Tony matches elements by their `name` field and
merges them structurally. The `sidecar` container is untouched because the
patch doesn't mention it.

### Diffing

Tony's `Diff(from, to)` computes minimal/small structural difference between two
documents. The output is itself a valid Tony document that moreover contains
the common ancestors of a fine grained diff, making it easier to read than
jsondiff which uses path references.

`Diff` uses tags to annotate what changed:

- **`!replace`** when types differ: `!replace { from: "old", to: "new" }`
- **`!delete`** for removed fields or elements
- **`!insert`** for added fields or elements
- **`!strdiff`** for character-level string changes
- **`!arraydiff`** for array changes with key matching
- **`!addtag` / `!rmtag` / `!retag`** for tag-only changes

Fields that are identical are omitted entirely. The diff is minimal.

For strings, The Go Tony `o` tool computes character-level diffs using the
diff-match-patch algorithm. But it's pragmatic: if the diff is larger than half
the size of the smaller string, it falls back to a simple `!replace`. No point
in a character-level diff that's harder to read than the replacement.

When arrays are tagged with `!key(field)`, diffing matches elements by key
value instead of position. Reordering doesn't produce noise---only actual
additions, removals, and modifications are reported:

```yaml
# from
containers: !key(name)
- name: app
  image: v1
- name: sidecar
  image: proxy:v1

# to
containers: !key(name)
- name: sidecar
  image: proxy:v2
- name: app
  image: v1

# diff - only the sidecar image changed, reordering is ignored
- name: sidecar
  image: !replace
    from: proxy:v1
    to: proxy:v2
```

For plain arrays (without `!key`), `!arraydiff` uses an abstracted
longest-common-subsequence to produce a positional diff with `!insert` and
`!delete` entries---and both the result and the per-item differences are valid
patches:

```yaml
# from
items:
- a
- b
- c

# to
items:
- a
- x
- c
- d

# diff
items: !arraydiff
  1: !replace
    from: b
    to: x
  3: !insert d
```

Diffs are reversible. `libdiff.Reverse(diff)` produces a diff that, when
applied to `to`, yields `from`:

```go
diff := Diff(a, b)
reversed, _ := libdiff.Reverse(diff)
// Patch(b, reversed) == a
```

The reversal logic is straightforward because the tags carry complete
information: `!delete` becomes `!insert` and vice versa, `!replace` swaps its
`from` and `to` fields, `!retag(x,y)` becomes `!retag(y,x)`.

## Composition

The real payoff is that these three operations form a closed loop.

**A diff is a valid patch.** The same tags that annotate changes (`!delete`,
`!insert`, `!replace`) are recognized by the patch engine. `Diff(a, b)`
produces output that you can pass directly to `Patch(a, diff)` to get `b`.
Reverse the diff and apply it to `b` to get `a`.

**Matching composes with patching.** `!if` uses match semantics in its
condition and patch semantics in its body. `!dive` matches to find subtrees
and patches to transform them. The `Op` interface makes this explicit---every
operation is both a matcher and a patcher, and the engine threads recursive
callbacks for both through the entire tree.

**Diffs compose with matching.** Because a diff is structured Tony data, you
can match against it, filter it, or patch it before applying it. A diff isn't
an opaque blob you feed to `patch`---it's a document you can inspect and
transform with the same tools you use on any other document.

This isn't three systems bolted together. It's one system and one
structure-preserving format with three entry points. Match, patch, and diff
are perspectives on the same tree-walking, tag-dispatching engine. The `Op`
interface, the IR, and the tag registry are shared all the way down.

## Conclusion

Three operations. One IR. One tag system. Matching, patching, and diffing
are the same operation viewed from different angles, and the model makes that
explicit. That's the whole idea.
