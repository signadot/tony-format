# tony: a line should end at its last non-space character or at a comment -- today only the block literal is strict, and about the wrong thing

A line in a Tony document should end at its last non-space character, or at a comment.
Neither half of that holds today, and the two failures point in opposite directions: the
block literal's opening line is the ONLY place that is strict, and it is strict about the
wrong thing.

MEASURED, on main, Tony mode (`parse.Parse` with `ParseTony()`):

    k: v            ok        k: v # c        ok
    k: v␣␣␣         ok  WRONG k: v␣␣ # c      ok
    {k: v␣␣}        ok  WRONG k: v # c␣␣␣     ok
    |               ok
    |␣              REFUSED   | # c           REFUSED  WRONG
    a: 1 / "␣␣" / b: 2        ok        (a whitespace-only line)
    k: | + "␣␣a␣␣" content    ok, value "a␣␣\nb\n"  (content keeps its whitespace)

The refusal is `unexpected` with an empty quoted byte, which tells a reader nothing:

    unexpected   at `...k: | # c\n...` at offset 4 (line=0, col=4)

WHERE IT COMES FROM. Two sites, both small:

  - token/mlit.go:71-74 -- mLit requires the byte after `|` (or after `|-` / `|+`) to be
    `\n`; anything else, space or `#` included, is UnexpectedErr. That is the strict half.
  - token/tokenizer.go:787-788 -- `case ' ', '\t', '\r', '\v', '\f': return nil, 1, nil`,
    "whitespace, no token". Whitespace consumes a byte and asks nothing about what follows,
    which is the lenient half: nothing can tell trailing whitespace from separating
    whitespace, so `k: v␣␣␣` is indistinguishable from `k:␣v`.

THE SPEC'S OWN EXAMPLE DOES NOT PARSE. docs/tony.md:215-220 is

    # same as " <\n^ leading space\n"
    |␣
       <␣␣␣
      ^ leading space

The `|␣` on line 217 is refused. Drop that one space and it parses, but to
`" <␣␣␣\n^ leading space\n"` -- the content line's trailing spaces are KEPT, where the
comment above it claims they are stripped. So the example is wrong twice, and the
prose around it ("Tony simply computes the expected indentation") is what the section is
really about; the whitespace claim is incidental and untrue.

For what it is worth, keeping them is what YAML does -- `yaml.safe_load("k: |\n  a  \n  b\n")`
gives `'a  \nb\n'` -- so the implementation is right and the doc is wrong, not the reverse.

NOT A REGRESSION. The behaviour is identical at the repository's initial commit 803a74f.

YAML MODE INHERITS THE REFUSAL, WHICH IS A COMPATIBILITY BUG. `ParseYAML()` refuses `k: |␣`
and `k: | # c` exactly as Tony mode does, and PyYAML accepts both (`{'k': 'a\n'}` for each).
A block-scalar header comment is ordinary in hand-written and generated YAML. Whatever is
decided for Tony mode, YAML mode should accept what YAML accepts.

WHAT A STRICT RULE WOULD COST, measured in this repository:

  - Six `.tony` files carry trailing whitespace. Three carry it only on whitespace-only
    lines -- including schema/base.tony and schema/schema.tony, so a rule with no blank-line
    carve-out stops parsing the schemas.
  - The other three carry it after a DANGLING COLON, not after a value:
    testdata/dir/build.tony:34 `spec:␣`, testdata/dir1/build.tony:58-59 `patch:␣` /
    `unquote:␣`, and the same two lines in gomap/codegen/testdata/pkg4/build.tony. The space
    there separates a key from an indented child; calling it trailing is a judgement, not a
    reading. (The dangling colon itself is 7ba8gz2eh12ksbwxe5n0.)
  - Seven testdata YAML files carry it after a value: t2-in, t4-in, t12-in, t15-out, t18-in,
    cluster_config_in, dir/source/cm.yaml. Real manifests look like this.

So the carve-outs are not decoration -- one of them is the schema files.

THE RULE I WOULD IMPLEMENT, if this is to be settled:

  1. In Tony mode, whitespace running to end-of-line after a value is an error.
  2. A whitespace-only line stays a blank line. There is no value for the whitespace to
     trail.
  3. Whitespace before a comment stays legal. `k: v␣␣ # c` is a comment, and a comment ends
     the line.
  4. A comment after `|` becomes legal, for the same reason: a comment ends any line, and
     the block literal is the only construct that says otherwise.
  5. YAML mode stays tolerant of all of it.
  6. Block-literal CONTENT lines are untouched -- they are inside the literal and are never
     tokenized as whitespace. Their trailing spaces are data (see the YAML agreement above);
     the doc at tony.md:216 is what needs fixing.

IMPLEMENTATION NOTE, because this is the part that bites. Refusing (1) needs lookahead from
the whitespace to the newline, and that lookahead can cross a read boundary. The whitespace
case returns `nil, 1, nil` today precisely because it needs nothing; a version that decides
on a short buffer will decide wrongly. It must return `io.EOF` to ask for a refill, which is
the same shape as the five boundary defects fixed in v0.0.158-162 (75g1kbpdh12krs09gdn0) --
"may more come?" not "is this valid?". It wants a read-size sweep in the manner of
token/streaming_boundary_test.go, not a spot test.

While in there: mLitStreaming (token/mlit_streaming.go:21-26) does NOT carry mLit's
`d[2] != '\n'` check after `|-` / `|+`. A read-size sweep over `k: |-x\n  a\n` refuses at
every read size today, so nothing is broken -- the drained path re-tokenizes through mLit --
but the two header parses should not differ in what they accept.

WHAT IS OPEN. Whether (1) is worth having at all. It is the only item that refuses documents
that parse today, its blast radius includes files in this repository, and the argument for
it is that a format which accepts invisible trailing bytes produces diffs nobody wrote.
Items (2)-(6) are strictly liberalizing or documentation and could land on their own.

Related: 6ykv73beh12krzeygsn0 asks the narrow question (is a comment allowed after `|`) and
is subsumed by item (4) here.