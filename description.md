# no fuzzer compares the parser with the wire decoder; a 10-minute run found 5 disagreements

The repo's fuzz targets don't reach the wire decoder:

- `parse.FuzzParse` only checks that the parser doesn't panic.
- `parse.FuzzEncodeParseRoundTrip` covers scalars only.
- The two `token` quoting fuzzers cover quoting.

The randomized logd tests (TestReadEquivalence and the others) build their patches with the encoder, which writes only canonical text. So no test sends unusual or malformed text through `stream.Decoder`. That is how parser and wire could disagree on every shape fixed on branch issue-v5dn9vfbh12krj (key sets, stray commas, a tag where a key goes, a doubled tag, a bare-tag document, a tag before a line break).

On 2026-10-31, after fe9d7a5c, a temporary differential fuzzer ran for about 10 minutes. The source is attached as `wire_agrees_with_parser_test.go`. For bracketed input, it requires that parse.Parse and stream.ReadDocument both refuse, or both produce the same wire encoding. Each class it found was then skipped so the next could surface:

1. **Mismatched closers.** The wire accepts `{]` as `{}`. The decoder never checks that a closer matches its opener.
2. **Stray ':'.** The wire drops it with the other structural tokens: `{:0}` reads as `{0: null}`.
3. **Trailing material.** The wire accepts `{0},`, `{0}:` and `{''},` and drops the `,` or `:` after the document. The parser refuses them: "trailing material".
4. **A space-separated trailing tag. The parser drops it.** `[0 !t]` parses as `[0]` and `[!t 0 !u]` as `[!t 0]`, losing an element. `[0, !t]` and `[0 !t 1]` parse correctly. The wire reads `[0 !t null]`.
5. **Number spelling.** `[0e0]` keeps its spelling in the parser and becomes `[0.0]` on the wire. The value is equal, so this may not be a defect. Large integers or long floats should be checked for real loss.

Not counted as a disagreement: the parser sorts sparse-array keys and the wire keeps their written order (`{2 0}`).

The run stopped while new classes were still turning up, so the list is incomplete.

Proposed: commit the differential fuzzer as a regular target in `stream`, fix the classes above, and keep found inputs in the seed corpus so `go test` replays them.