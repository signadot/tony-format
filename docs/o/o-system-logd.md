# o system logd

[logd](https://signadot.github.io/tony-format/logd/) storage server commands

## Usage

```
o system logd <subcommand>
```

## Commands

| command | synopsis |
| --- | --- |
| [`serve`](o-system-logd-serve.md) | `o system logd serve -data <dir> [-addr <addr>] [-admin-addr <addr>]` |
| [`session`](o-system-logd-session.md) | `o system logd session <addr>` |

## Options

### inherited from `o`

| option | type | default | description |
| --- | --- | --- | --- |
| `-b` | bool |  | encode with brackets |
| `-x` | bool |  | expand <<: merge field while encoding |
| `-color` | bool |  | colorize; on by default to a terminal, -color=false to suppress |
| `-wire` | bool |  | output in compact format |
| `-h`, `-help` | bool |  | show help for this command |
| `-t`, `-tony` | bool |  | do i/o in tony |
| `-j`, `-json` | bool |  | do i/o in json |
| `-y`, `-yaml` | bool |  | do i/o in yaml |
| `-o` | (filepath) |  | output file (default stdout) |
| `-I`, `-ifmt` | (format) |  | input format: tony/t, json/j, yaml/y |
| `-O`, `-ofmt` | (format) |  | output format: tony/t, json/j, yaml/y |

Inherited options may be given either before or after the command they are inherited by.

Boolean options take no argument and may be negated with a `no-` prefix, as in `-no-debug`.

## See also

- [published documentation](https://signadot.github.io/tony-format/o/o-system-logd/)
- [`o system`](o-system.md)
- [`o system docd`](o-system-docd.md)
- [`o system up`](o-system-up.md)

