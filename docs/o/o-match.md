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
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

### `o match` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments in the answer; matching is blind to them either way |
| `-trim` | bool |  | trim the results to the match |
| `-s` | bool |  | consider match a string argument |
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
- [`o version`](o-version.md)

