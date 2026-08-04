# encode: numbers do not round-trip — integral floats retype, large floats emit unparseable output

Numbers do not survive an encode/decode round trip. Four distinct faces, in severity
order. Checked at `ae31b68` and with `o` built from it.

## 1. The encoder emits documents tony cannot parse

```
k: 1.7976931348623157e308
```

```
$ o view big.tony > big2.tony      # succeeds
$ o view big2.tony
error decoding document 0: invalid integer strconv.ParseInt: parsing
"179769313486231570000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000000000000000000000
0000000000000000000000000000000000000000000000000000000000000000000000000000000000
000000000000000000000000000000000000000000": value out of range
```

The float is written as its full 309-digit decimal expansion, with no `.` or exponent, so
it reads back as an integer token and overflows `int64`. Encode is not a total function
into the language it encodes.

## 2. Integral floats silently retype to integers

```
1.0     -> 1        float -> int
1e2     -> 100      float -> int
3.0e2   -> 300      float -> int
```

`o diff doc <o view doc>` is non-empty for each: the document is not equal to itself after
a round trip. The diff renders as a no-op, which is its own confusion:

```
k: !replace
  from: 1
  to: 1
```

Cause is the encoder alone. The IR is right — `ir.FromFloat` sets `Float64`
(`ir/node.go:122`) and `ir.FromInt` sets `Int64` (`ir/node.go:114`), so a float node and an
int node are correctly unequal. It is the rendering that drops the distinction, and
reparsing then commits it.

## 3. `-0.0` breaks the equal-implies-same-hash invariant

`ir/node.go:426` compares floats with `!=`, where Go says `-0.0 == 0.0`. `ir/hash.go:50`
hashes `math.Float64bits`, where the sign bit differs:

```
a = ir.FromFloat(-0.0), b = ir.FromFloat(0.0)
DeepEqual(a,b) = true
Hash(a)        = 5951978445854759468
Hash(b)        = 5952119183343170476
hashes equal   = false
```

Two nodes that are equal hash differently. Anything keyed on `Hash()` — logd storage in
particular — can hold both as distinct entries while every comparison says they are the
same value.

`-0.0` is reachable from source: `k: -0.0` parses, and encodes as `0.0`, so the sign is
also lost on the way out (face 2's mechanism, different consequence).

## 4. `1e400` is rejected on input, against the JSON compatibility claim

```
$ o view <<< 'k: 1e400'
error decoding document 0: invalid err strconv.ParseFloat: parsing "1e400":
value out of range
```

`docs/tony.md:10` says "Valid JSON is valid Tony with no changes in semantics." `{"k":
1e400}` is valid JSON. `parse/parse.go:257,464` route every float through
`strconv.ParseFloat(..., 64)`, so anything outside float64 range is refused.

Whether tony wants arbitrary-precision numbers is a real question and this issue does not
assume the answer — but the spec currently promises something the parser does not deliver,
and one of the two has to move.

## Why this blocks v0tp11hmh12krncwe5n0

That issue proposes carrying integer/float notation (`!hex`, `!oct`, `!bin`, `!exp`) as
presentation tags and specifies a canonical rendering per notation per output format. That
is a rendering contract for numbers, and it cannot be specified — let alone tested — on top
of an encoder whose existing number rendering is neither injective nor total.

Concretely: `!exp` is meant to make `1e9` survive as `1e9`. Today `1e9` renders as
`1000000000` and comes back an integer. The notation work would be built on that.

It also undercuts the test that issue relies on. `TestNeedsQuoteAgreesWithRoundTrip` is
named as the mechanism that keeps `NeedsQuote` honest as the literal rules change; a
round-trip oracle is worth less while round-tripping is broken for a neighbouring type.

Best handled on its own, before or in parallel with that issue's stage 0.

## Suggested shape

- Floats render in a form that reads back as a float: keep a `.0` or an exponent, never a
  bare integer.
- Large-magnitude floats render in exponent form rather than decimal expansion, which fixes
  face 1 and is what face 2's `!exp` work wants anyway.
- Either make `DeepEqual` sign-aware for zero or make `Hash` normalize it — but pick one, and
  add the property test that equal nodes hash equally.
- Decide the `1e400` question and make `docs/tony.md:10` and the parser agree.

Worth a property test over generated numbers: `Parse(Encode(n))` equals `n` and hashes
equal, which is what would have caught all four.