# numbers: int64/float64 bounds reject valid JSON — wants big.Int/big.Float

Split out of 6fnd2hxeh12ks359e5n0, where it was the one face not fixed.
Checked at b50645f.

`docs/tony.md` says a Tony number is a JSON number and puts no bound on its
magnitude or precision.  The implementation reads integers as int64 and
everything else as float64 and refuses what does not fit:

```
1e400                           rejected   beyond float64
123456789012345678901234567890  rejected   beyond int64
9223372036854775808             rejected   int64 max + 1
-9223372036854775809            rejected   int64 min - 1
```

All four are valid JSON.  `docs/tony.md` now records this under Number Range as
a limitation of the implementation rather than a property of the format, and
`TestNumberRangeLimitation` in `parse/number_roundtrip_test.go` pins the current
behaviour, so whatever lands here has to change that test deliberately.

Rejecting is the conservative reading meanwhile, and arguably better than the
neighbours: Go's `encoding/json` rejects `1e400` but silently rounds the thirty
digit integer to `1.2345678901234568e+29`.

## Where the bound is

Five parses, all 64-bit:

- `parse/parse.go:252` `ParseInt(…, 10, 64)` — scalar value
- `parse/parse.go:259` `ParseFloat(…, 64)` — scalar value
- `parse/parse.go:450` `ParseInt(…, 10, 64)` — value, other arm
- `parse/parse.go:467` `ParseFloat(…, 64)` — value, other arm
- `parse/parse.go:628` `ParseUint(…, 10, 64)` — integer map key

## The Number field is not the ready-made path it looks like

`ir.Node.Number` holds number text and every comparison already falls back to
it — `ir/hash.go:62`, `ir/compare.go:86`, `ir/node.go:415`, `match.go:138`,
`mergeop/raw.go:130` — which makes it look like arbitrary precision is half
built.  It is not.  Every *consumer* narrows it straight back to 64 bits:

- `gomap/from.go:307,385,458,550` re-parse it with `ParseInt`/`ParseUint`/`ParseFloat`
- `eval/to_int.go:54` the same
- `gomap/type_extract.go:214` the same
- `ir/compare.go:86` orders it with `strings.Compare`, so ordering is textual:
  "9" sorts after "10"

And the encoder does not handle it at all.  A `NumberType` node carrying only
`Number` writes an empty value, with no error:

```go
n := &ir.Node{Type: ir.NumberType, Number: "123456789012345678901234567890"}
// encode.Encode(FromMap{"k": n, "z": "after"}) =>
//   err=<nil>
//   output="k: \nz: after\n"
```

That output does not parse:

```
imbalanced document: extraneous indent (no comment) TLiteral "z" … (line=1, col=0)
```

So the fallback path is silently lossy today, before any big number work.  That
is worth fixing whichever way this issue goes, since `Number` is reachable now
through `ir/ir_json.go` — a serialized node round-tripped through JSON.

## Scope

Whatever representation is chosen — `math/big` on the node, or making `Number`
authoritative with the 64-bit fields as a fast path — the work is in the
consumers rather than the parse sites:

- `encode`: render it, and stop writing an empty value
- `ir`: hash, `DeepEqual`, and `compare` need numeric rather than textual
  ordering, or the sort order stays wrong for the fallback
- `gomap`: overflow into a fixed-width Go field has to report rather than round
- `schema`, `eval`, `match`, `mergeop`
- integer map keys, which `docs/tony.md` separately restricts to base-10 and
  32 bits

`ir/hash.go` also wants care: two numbers that are `DeepEqual` must hash
equally, which is what 6fnd2hxeh12ks359e5n0 fixed for signed zero and is easy to
break again with a second representation of the same value — big "1" and int64 1
must not hash apart.