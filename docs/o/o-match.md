# o match

match objects documents with match documents

Each document is matched whole: a file holding a list is one document, and the
pattern is asked about the list rather than about its elements. A file holding
several documents separated by --- is matched one at a time, and the ones which
match are written, so match reads as a filter over a document stream.

A file is optional: with none, stdin is read, so "x | o m PAT" needs no
trailing -.

Exit codes follow grep, so that a pipe can tell an answer from a fault:

  0  something matched and was written
  1  nothing matched -- an answer, not an error, and written on no stream
  2  a fault: bad usage, unreadable input, an unparseable match document

A comment describes a value and is not what the value IS, so a match sees through
one: {kind: Deployment} matches a document somebody wrote a note above. !comment
is how a pattern asks about the comments themselves, and it needs -c -- without
it the comments are never read and there is nothing to ask about:

    o m -c '!comment {head: ["# generated"]}' *.tony
    o m -c '!comment {head: []}' *.tony   # the ones with nothing written above

It asks about the comments and not about the value, so a pattern wanting both is
the composition it looks like:

    o m -c '!and [!comment {head: ["# generated"]}, {kind: Deployment}]'

o match -tags lists the operators a match may use, from this binary. What each one
means, and how match and patch share a vocabulary, is at
<https://signadot.github.io/tony-format/matchpatch/>

Also known as `m`.

## Usage

```
o match [opts] <matchobj> [files]
```

## Options

### inherited from `o`

| option | type | default | description |
| --- | --- | --- | --- |
| `-b` | bool |  | encode with brackets |
| `-x` | bool |  | expand <<: merge field while encoding |
| `-color` | bool |  | encode with color |
| `-wire` | bool |  | output in compact format |
| `-h`, `-help` | bool |  | show help for this command |
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

### `o match` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments in the answer, and let a !comment pattern see them |
| `-trim` | bool |  | trim the results to the match |
| `-f` | bool |  | consider match a file path |
| `-tags` | bool |  | show available tags |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-match/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o get`](o-get.md)
- [`o list`](o-list.md)
- [`o patch`](o-patch.md)
- [`o build`](o-build.md)
- [`o dump`](o-dump.md)
- [`o load`](o-load.md)
- [`o schema`](o-schema.md)
- [`o system`](o-system.md)
- [`o docs`](o-docs.md)
- [`o help`](o-help.md)
- [`o completion`](o-completion.md)
- [`o version`](o-version.md)

