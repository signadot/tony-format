# parse: a tag name is terminated only by whitespace, so `,`, `}` and `]` are absorbed into it

# parse: a tag name is terminated only by whitespace, so `,`, `}` and `]` are absorbed into it

A tag runs to the next whitespace and nothing else stops it, so a structural character that follows
one immediately becomes part of the tag NAME. Three of the four shapes below are wrong, and the
first is wrong SILENTLY.

```
{a !delete, b: 1}    tag = "!delete,"      no such operator; stored as data, `a` is NOT deleted
{a !delete}          parse error: '{' not closed
[a, !delete]         parse error: '[' not closed
{a !delete }         tag = "!delete"       correct — the only spelling that works
```

The four differ by one character, and the correct one is the one with a trailing space before the
brace, which no reviewer sees and no formatter preserves visibly.

## The silent one

`{a !delete, b: 1}` parses to a field `a` holding a Null tagged `!delete,`. `mergeop.Lookup`
finds nothing for `delete,`, and an unknown tag is passed through as data — so the patch stores
the marker instead of performing the removal:

```
before  {a: yes, keep: me}
patch   {a !delete, keep: me}
after   {a: !delete, keep: me}      <- `a` still stands, now holding an operator-tagged null
```

Downstream this is worse than a no-op. Consumers guard the write path by asking whether a tag
names a known non-patch operator (verse does this twice, in `entity.CheckPatchable` and via
`logdapi.ValidateForStorage`); an UNKNOWN tag passes both, because neither has anything to look
up. So the document lands carrying a live-looking operator tag that no reader will execute and no
guard will refuse.

## Where it comes from

The tag production is YAML's — any non-whitespace unicode — inherited wholesale. That is right for
YAML, where a tag is a URI, and wrong here, where `,`, `[`, `]`, `{` and `}` all belong to the
flow grammar the tag sits inside.

## What NOT to do

The obvious fix — terminate on `, [ ] { }` — breaks two things that work today and are wanted:

```
!get-path(a[0])   tag="!get-path(a[0])"  head="!get-path"  args=[a[0]]   a kpath with an index
!tag(a,b)         tag="!tag(a,b)"        head="!tag"       args=[a b]    a multi-arg component
```

A component is where a bracket or a comma is legitimate content, and taking them away costs an
addressing form (`!get-path(a[0])`) that has no other spelling.

## Proposed: terminate on structural characters AT PAREN DEPTH 0

Track `(`/`)` nesting while scanning the tag; terminate on `, [ ] { }` only at depth 0, and
accept anything inside a component. That fixes all three defects and keeps both capabilities:

| input | today | proposed |
|---|---|---|
| `{a !delete, b: 1}` | tag `!delete,` — silent | tag `!delete`, `,` separates |
| `{a !delete}` | `'{' not closed` | tag `!delete`, `}` closes |
| `[a, !delete]` | `'[' not closed` | tag `!delete`, `]` closes |
| `{a !delete }` | tag `!delete` | unchanged |
| `!get-path(a[0])` | args `[a[0]]` | unchanged |
| `!tag(a,b)` | args `[a b]` | unchanged |

An unmatched `(` degrades to today's behaviour — the scan runs to whitespace — so nothing that
parses now stops parsing except the three rows above, each of which currently either errors or
does the wrong thing.

The parser already validates tags in other ways: it refuses two tags on one node (`!not !irtype`
→ *tags compose with '.', as !not.or*) and refuses a tagged key (*key cannot be tagged*). Letting
a separator into a tag name looks like an oversight in the scan rather than a rule.

## Adjacent, and not proposed here

`{a !delete b}` parses as `{a: !delete null, b: null}` — the tag binds to the current key's value,
and a bare word is a key with a null value. Both rules are self-consistent and the combination
reads backwards (it deletes `a` and sets `b: null`), but nothing here is malformed, so it is a
documentation question rather than a scanner one.

Found while looking for a shorter spelling of `{stage: unassigned, decided: !delete null}` in a
verse charter patch. The short form exists — `{stage: unassigned, decided !delete }` — and is
exactly equivalent; the surrounding near-misses are what this issue is about.