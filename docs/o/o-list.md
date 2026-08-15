# o list

list or query objects elements from files

The path says WHERE to look and -if says WHICH of what is there to keep, which
together are how a list is filtered by a match:

    x | o list -if '{state: open}' '$[*]'            # the matching elements
    o list -if '{state: open}' '$.items[*]' doc.tony # at any depth the path reaches

-trim writes only the parts its own match document names, so which nodes and how
much of each are asked separately:

    x | o list -if '{status: running}' -trim '{runner: null, started: null}' '[*]'

A file is optional: with none, stdin is read, as grep and cat do -- from a pipe,
or typed at a terminal and ended with Ctrl-D.

Without -if the path is the whole question and every node it names is written.
The answer is a list, and the empty list is written as one; the exit code says
whether it was empty: 0 when something was kept, 1 when nothing was, 2 for a
fault.

Also known as `l`.

## Usage

```
o list [opts] <objectpath> [files]
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

### `o list` options

| option | type | default | description |
| --- | --- | --- | --- |
| `-if` | string |  | keep only the nodes matching this match document |
| `-if-file` | string |  | read the match document from a file |
| `-trim` | string |  | write only the parts this match document names |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-list/)
- [`o`](README.md)
- [`o view`](o-view.md)
- [`o eval`](o-eval.md)
- [`o diff`](o-diff.md)
- [`o get`](o-get.md)
- [`o match`](o-match.md)
- [`o patch`](o-patch.md)
- [`o build`](o-build.md)
- [`o dump`](o-dump.md)
- [`o load`](o-load.md)
- [`o schema`](o-schema.md)
- [`o system`](o-system.md)
- [`o docs`](o-docs.md)
- [`o version`](o-version.md)

