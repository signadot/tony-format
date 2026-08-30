# o system docd

[docd](https://signadot.github.io/tony-format/docd/) document server commands

## Usage

```
o system docd <subcommand>
```

## Commands

| command | synopsis |
| --- | --- |
| [`serve`](o-system-docd-serve.md) | `o system docd serve [-addr <addr>] [-mount-addr <addr>] [-logd <addr>] [-admin-addr <addr>]` |
| [`mounts`](o-system-docd-mounts.md) | `o system docd mounts [-addr <addr>]` |
| [`schema`](o-system-docd-schema.md) | `o system docd schema [-addr <addr>]` |

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

- [published documentation](https://signadot.github.io/tony-format/o/o-system-docd/)
- [`o system`](o-system.md)
- [`o system logd`](o-system-logd.md)
- [`o system up`](o-system-up.md)

