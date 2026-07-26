# kpath: no wildcard matching between paths — [*]/.* compare literally, and Wild() is head-segment-only

Found while designing verse's process ids as kpaths (a spawned process's id carries the
kpath of the action that spawned it inside its trigger, e.g. `review.seq[2].onDone`), where
the natural query is "every process under `review.seq[*]`".

Observed against go-tony v0.0.90, `ir/kpath`.

## 1. A wild path is not an ancestor of the paths it denotes

`AncestorOrEqual`/`IsPrefix` compare segments with `segmentsEqual` (ir/kpath/segment.go:52),
which is strict equality: `IndexAll == IndexAll`, `FieldAll == FieldAll`. So a wildcard
segment matches only another wildcard segment, never the concrete segment it stands for.

    pattern                 target                    anc    eq
    review.seq[*]           review.seq[2]             false  false
    review.seq[*].onDone    review.seq[2].onDone      false  false
    review.*                review.onDone             false  false
    review.seq[2]           review.seq[2].onDone      true   false   (concrete prefix — correct)
    review                  review.seq[2].onDone      true   false   (concrete prefix — correct)

Wildcards parse, round-trip through `String()`, and are honored when *navigating a document*
(ir/kpath.go:76-82) — but there is no path-against-path predicate, so a caller holding a
wildcard path and a concrete path cannot ask whether one denotes the other without
re-implementing segment matching.

Ask: `func (p *KPath) Matches(o *KPath) bool` (and probably `MatchesPrefix`), segment-wise,
where `.*` / `[*]` / `{*}` match any segment of the same `EntryKind` and concrete segments
compare as they do today. It is small, but it belongs next to `AncestorOrEqual` rather than
in every consumer.

## 2. `Wild()` on a parsed path answers about the first segment only

`Wild()` (ir/kpath/kpath.go:139) is `p.FieldAll || p.IndexAll || p.SparseIndexAll` — a
*segment* predicate, which is consistent with `Type()` returning a `SegmentType`. But
`Parse` hands back a `*KPath` that reads as a whole path, and the same type serves as both
path and segment, so:

    kp, _ := kpath.Parse("review.seq[*]")
    kp.Wild()   // false — silently answers "is the HEAD segment wild"

Nothing warns. Ask: a whole-path `HasWild()` (any segment), with `Wild()` documented
explicitly as head-segment-only — or `Wild()` promoted to whole-path with a
`LastSegment().Wild()` idiom for the segment question.

## 3. Question, not a defect: is there an element wildcard that spans dense and keyed?

`[*]` is `ArrayEntry` and `(name)` is `KeyEntry`, so under a kind-typed matcher `[*]` would
not match `(lint)`. For a list whose elements may be addressed either positionally or by
name, is the intent that a caller must ask twice (`[*]` and a key wildcard, if one exists —
there is no `(*)` in the syntax today), or is a kind-spanning "any element" wildcard in
scope? This affects whether a keyed form is usable as a queryable address.

## Downstream workaround

verse will keep chain queries on an entity payload field (plain equality matching) rather
than globbing ids, and hand-roll a segment-wise matcher if it needs one before this lands.
Note also that `filepath.Match` is the wrong matcher for kpath-shaped strings — `[2]` reads
as a character class — so any glob-style operator over these ids is wrong by default.