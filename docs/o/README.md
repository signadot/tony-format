# o

o is a tool for working with object notation.

## Usage

```
o [opts] command [opts]
```

## Commands

| command | synopsis |
| --- | --- |
| [`view`](o-view.md) | `o view [files]` |
| [`eval`](o-eval.md) | `o eval [-e path=val [ -e path2=val2 ]...] [files]` |
| [`diff`](o-diff.md) | a b or diff -loop <cmd> |
| [`get`](o-get.md) | `o get [opts] <kpath> [files]` |
| [`list`](o-list.md) | `o list [opts] <kpath> [files]` |
| [`match`](o-match.md) | `o match [opts] <matchobj> [files]` |
| [`patch`](o-patch.md) | `o patch [opts] <patchobj> [files]` |
| [`build`](o-build.md) | `o build [dir] [-l] [-p profile ] [ env ]` |
| [`dump`](o-dump.md) | `o dump [files]` |
| [`load`](o-load.md) | `o load [ir-files]` |
| [`schema`](o-schema.md) | `o schema <subcommand>` |
| [`system`](o-system.md) | `o system <subcommand>` |
| [`docs`](o-docs.md) | `o docs <dir>` |
| [`help`](o-help.md) | `o help [command]` |
| [`completion`](o-completion.md) | bash\|zsh\|fish |
| [`version`](o-version.md) | version |

## Options

### `o` options

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

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/)

