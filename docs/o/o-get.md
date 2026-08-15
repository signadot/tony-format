# o get

get objects elements from files

The path says WHERE to look. -if says WHICH of what is there to keep: the node
is written only when it matches the match document given, so

    o get -if '{state: open}' '$.items[0]' doc.tony && deploy

reads as the guard it is. -trim writes only the parts its own match document
names. A file is optional: with none, stdin is read.

Exit codes are the search convention: 0 when something was written, 1 when the
path named nothing or what it named did not match, 2 for a fault.

Also known as `g`, `ge`.

## Usage

```
o get [opts] <objectpath> [files]
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

### `o get` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-c` | bool |  | include comments |
| `-if` | string |  | keep the node only if it matches this match document |
| `-if-file` | string |  | read the match document from a file |
| `-trim` | string |  | write only the parts this match document names |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-get/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o list`](o-list.md)
- [`o match`](o-match.md)
- [`o patch`](o-patch.md)
- [`o build`](o-build.md)
- [`o dump`](o-dump.md)
- [`o load`](o-load.md)
- [`o schema`](o-schema.md)
- [`o system`](o-system.md)
- [`o docs`](o-docs.md)
- [`o version`](o-version.md)

